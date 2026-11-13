package signing_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"filippo.io/edwards25519"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/signing"
)

func TestRequestSignatureBindsEveryField(t *testing.T) {
	t.Parallel()
	var challenge [signing.ChallengeLen]byte
	copy(challenge[:], bytes.Repeat([]byte{7}, signing.ChallengeLen))
	body := []byte(`{"id":"x"}`)
	signature := signing.SignRequest(
		core.NewSecret(),
		signing.PurposeRead,
		"POST",
		"/api/v1/abuse",
		challenge,
		body,
		signing.Components{},
	)
	if !signature.Verify(signing.PurposeRead, "POST", "/api/v1/abuse", body, signing.Components{}) {
		t.Fatal("the signature does not verify")
	}
	otherChallenge := *signature
	otherChallenge.Challenge[0] ^= 1
	for field, verified := range map[string]bool{
		"purpose":   signature.Verify(signing.PurposeManage, "POST", "/api/v1/abuse", body, signing.Components{}),
		"method":    signature.Verify(signing.PurposeRead, "GET", "/api/v1/abuse", body, signing.Components{}),
		"path":      signature.Verify(signing.PurposeRead, "POST", "/api/v1/abuses", body, signing.Components{}),
		"body":      signature.Verify(signing.PurposeRead, "POST", "/api/v1/abuse", []byte(`{"id":"y"}`), signing.Components{}),
		"challenge": otherChallenge.Verify(signing.PurposeRead, "POST", "/api/v1/abuse", body, signing.Components{}),
	} {
		if verified {
			t.Fatalf("a signature verified with another %s", field)
		}
	}
	nonCanonical := strings.Repeat("A", 43) + "." + strings.Repeat("A", 48) + "." + strings.Repeat("A", 85) + "B"
	for _, header := range []string{"", "Bearer x", signing.AuthScheme + " a.b", signing.AuthScheme + " " + nonCanonical} {
		if _, err := signing.ParseAuthorization(header); !errors.Is(err, signing.ErrAuthorization) {
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
		if signing.ValidPublicKey(key) {
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
		if signing.ValidPublicKey(key) {
			t.Fatalf("accepted the non-canonical key %s", name)
		}
	}
	notOnCurve := make([]byte, 32)
	for notOnCurve[0] = 2; isPoint(notOnCurve); notOnCurve[0]++ {
	}
	if signing.ValidPublicKey(notOnCurve) || signing.ValidPublicKey(identity[:31]) {
		t.Fatal("accepted a value that is not a point or not 32 bytes")
	}
	derived := signing.PublicKey(core.NewSecret())
	if !signing.ValidPublicKey(derived[:]) {
		t.Fatalf("rejected a derived key %x", derived)
	}
	mixed, _ := new(edwards25519.Point).SetBytes(derived[:])
	torsion, _ := new(edwards25519.Point).SetBytes(order8a)
	if !signing.ValidPublicKey(new(edwards25519.Point).Add(mixed, torsion).Bytes()) {
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

func TestRequestSignatureBindsSecurityHeaders(t *testing.T) {
	t.Parallel()
	seed := core.NewSecret()
	challenge := [signing.ChallengeLen]byte{7}
	original := signing.Components{ShareKey: "share-key", Lease: "lease-token", KE1: "ke1", KE3: "ke3"}
	for _, purpose := range []string{signing.PurposeRead, signing.PurposeManage} {
		signature := signing.SignRequest(seed, purpose, "POST", "/claim", challenge, nil, original)
		if !signature.Verify(purpose, "POST", "/claim", nil, original) {
			t.Fatal("unchanged components did not verify")
		}
		for _, altered := range []signing.Components{
			{ShareKey: "other", Lease: original.Lease, KE1: original.KE1, KE3: original.KE3},
			{ShareKey: original.ShareKey, Lease: "other", KE1: original.KE1, KE3: original.KE3},
			{ShareKey: original.ShareKey, Lease: original.Lease, KE1: "other", KE3: original.KE3},
			{ShareKey: original.ShareKey, Lease: original.Lease, KE1: original.KE1, KE3: "other"},
			{ShareKey: original.ShareKey, Lease: original.Lease, KE1: original.KE3, KE3: original.KE1},
			{Lease: original.Lease},
			{ShareKey: original.ShareKey},
			{},
			{ShareKey: original.Lease, Lease: original.ShareKey, KE1: original.KE1, KE3: original.KE3},
		} {
			if signature.Verify(purpose, "POST", "/claim", nil, altered) {
				t.Fatalf("%s accepted altered components %+v", purpose, altered)
			}
		}
		empty := signing.SignRequest(seed, purpose, "POST", "/claim", challenge, nil, signing.Components{})
		if empty.Verify(purpose, "POST", "/claim", nil, original) {
			t.Fatal("unsigned headers could be injected")
		}
	}
	// Delimiters inside values cannot move bytes from one component to another.
	one := signing.RequestMessage(
		"read",
		"GET",
		"/",
		&challenge,
		nil,
		signing.Components{ShareKey: "a\x00b", Lease: "c"},
	)
	two := signing.RequestMessage(
		"read",
		"GET",
		"/",
		&challenge,
		nil,
		signing.Components{ShareKey: "a", Lease: "b\x00c"},
	)
	if bytes.Equal(one, two) {
		t.Fatal("ambiguous component framing")
	}
}

func TestRequestSignatureRejectsLegacyTranscript(t *testing.T) {
	t.Parallel()
	seed := core.NewSecret()
	challenge := [signing.ChallengeLen]byte{7}
	bodyHash := sha256.Sum256(nil)
	legacy := []byte("nylzo/v1/request\x00read\x00GET\x00/api/v1/messages/id\x00")
	legacy = append(legacy, challenge[:]...)
	legacy = append(legacy, bodyHash[:]...)
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	defer clear(privateKey)
	signature := signing.Signature{PublicKey: signing.PublicKey(seed), Challenge: challenge}
	copy(signature.Value[:], ed25519.Sign(privateKey, legacy))
	if signature.Verify("read", "GET", "/api/v1/messages/id", nil, signing.Components{}) {
		t.Fatal("legacy transcript accepted")
	}
}
