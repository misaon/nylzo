package core_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"filippo.io/edwards25519"

	"github.com/misaon/nylzo/internal/core"
)

func TestRequestSignatureBindsEveryField(t *testing.T) {
	t.Parallel()
	var challenge [core.ChallengeLen]byte
	copy(challenge[:], bytes.Repeat([]byte{7}, core.ChallengeLen))
	body := []byte(`{"id":"x"}`)
	signature := core.SignRequest(core.NewSecret(), core.PurposeRead, "POST", "/api/v1/abuse", challenge, body)
	if !signature.Verify(core.PurposeRead, "POST", "/api/v1/abuse", body) {
		t.Fatal("the signature does not verify")
	}
	otherChallenge := signature
	otherChallenge.Challenge[0] ^= 1
	for field, verified := range map[string]bool{
		"purpose":   signature.Verify(core.PurposeManage, "POST", "/api/v1/abuse", body),
		"method":    signature.Verify(core.PurposeRead, "GET", "/api/v1/abuse", body),
		"path":      signature.Verify(core.PurposeRead, "POST", "/api/v1/abuses", body),
		"body":      signature.Verify(core.PurposeRead, "POST", "/api/v1/abuse", []byte(`{"id":"y"}`)),
		"challenge": otherChallenge.Verify(core.PurposeRead, "POST", "/api/v1/abuse", body),
	} {
		if verified {
			t.Fatalf("a signature verified with another %s", field)
		}
	}
	nonCanonical := strings.Repeat("A", 43) + "." + strings.Repeat("A", 48) + "." + strings.Repeat("A", 85) + "B"
	for _, header := range []string{"", "Bearer x", core.AuthScheme + " a.b", core.AuthScheme + " " + nonCanonical} {
		if _, err := core.ParseAuthorization(header); !errors.Is(err, core.ErrAuthorization) {
			t.Fatalf("%q was accepted", header)
		}
	}
}

func TestValidPublicKeyRejectsDegenerateKeys(t *testing.T) {
	t.Parallel()
	filled := func(first, last byte) []byte {
		key := bytes.Repeat([]byte{0xff}, 32)
		key[0], key[31] = first, last
		return key
	}
	withSign := func(key []byte) []byte {
		signed := bytes.Clone(key)
		signed[31] |= 0x80
		return signed
	}
	identity := append([]byte{1}, make([]byte, 31)...)
	order8a, _ := hex.DecodeString("26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05")
	order8b, _ := hex.DecodeString("c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a")
	smallOrder := map[string][]byte{
		"identity":  identity,
		"order 2":   filled(0xec, 0x7f),
		"order 4":   make([]byte, 32),
		"order 8 a": order8a,
		"order 8 b": order8b,
	}
	for name, key := range smallOrder {
		if !isSmallOrder(key) {
			t.Fatalf("%s is not a small-order point, the test data is wrong", name)
		}
		if core.ValidPublicKey(key) {
			t.Fatalf("accepted the small-order key %s", name)
		}
	}
	nonCanonical := map[string][]byte{
		"y = p":              filled(0xed, 0x7f),
		"y = p + 1":          filled(0xee, 0x7f),
		"identity with sign": withSign(identity),
		"order 2 with sign":  withSign(filled(0xec, 0x7f)),
	}
	for offset := byte(2); offset <= 18; offset++ {
		if key := filled(0xed+offset, 0x7f); isPoint(key) && !isSmallOrder(key) {
			nonCanonical[fmt.Sprintf("y = p + %d", offset)] = key
		}
	}
	if len(nonCanonical) == 4 {
		t.Fatal("no non-canonical encoding of a point outside the small-order group, the test data is wrong")
	}
	for name, key := range nonCanonical {
		if core.ValidPublicKey(key) {
			t.Fatalf("accepted the non-canonical key %s", name)
		}
	}
	notOnCurve := make([]byte, 32)
	for notOnCurve[0] = 2; isPoint(notOnCurve); notOnCurve[0]++ {
	}
	if core.ValidPublicKey(notOnCurve) || core.ValidPublicKey(identity[:31]) {
		t.Fatal("accepted a value that is not a point or not 32 bytes")
	}
	derived := core.PublicKey(core.NewSecret())
	if !core.ValidPublicKey(derived[:]) {
		t.Fatalf("rejected a derived key %x", derived)
	}
	mixed, _ := new(edwards25519.Point).SetBytes(derived[:])
	torsion, _ := new(edwards25519.Point).SetBytes(order8a)
	if !core.ValidPublicKey(new(edwards25519.Point).Add(mixed, torsion).Bytes()) {
		t.Fatal("rejected a key of mixed order, which no third party can forge for")
	}
}

func isPoint(key []byte) bool {
	_, err := new(edwards25519.Point).SetBytes(key)
	return err == nil
}

func isSmallOrder(key []byte) bool {
	point, err := new(edwards25519.Point).SetBytes(key)
	return err == nil && new(edwards25519.Point).MultByCofactor(point).Equal(edwards25519.NewIdentityPoint()) == 1
}

func BenchmarkValidPublicKey(b *testing.B) {
	key := core.PublicKey(core.NewSecret())
	for b.Loop() {
		core.ValidPublicKey(key[:])
	}
}

func BenchmarkVerifyRequest(b *testing.B) {
	const path = "/api/v1/messages/AAAAAAAAAAAAAAAAAAAAAA"
	var challenge [core.ChallengeLen]byte
	signature := core.SignRequest(core.NewSecret(), core.PurposeRead, "GET", path, challenge, nil)
	for b.Loop() {
		if !signature.Verify(core.PurposeRead, "GET", path, nil) {
			b.Fatal("the signature does not verify")
		}
	}
}
