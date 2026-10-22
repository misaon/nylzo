package core_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"filippo.io/edwards25519"

	"github.com/misaon/nylzo/internal/core"
)

func buildSample(kdf core.KDF, pw string, file []byte) (core.ID, *[32]byte, core.ContentKeys, core.Header, core.Policy, []byte, error) {
	id := core.NewID()
	s := core.NewSecret()
	_, content, err := core.DeriveAll(id, s, kdf, pw)
	if err != nil {
		return id, s, content, core.Header{}, core.Policy{}, nil, err
	}
	h := core.NewHeader(kdf)
	pol := core.Policy{OpensMax: 1, TTLSeconds: 86400}
	text := "integrity"
	payload := core.Payload{Meta: core.Meta{Text: &text}}
	if file != nil {
		payload.Meta.File = &core.FileMeta{Name: "f.bin", Mime: "application/octet-stream", Size: int64(len(file))}
		payload.File = file
	}
	object, err := core.Seal(&content.K, h, id, pol, payload)
	return id, s, content, h, pol, object, err
}

func sampleMessage(t *testing.T, kdf core.KDF, pw string, file []byte) (core.ID, *[32]byte, core.ContentKeys, core.Header, core.Policy, []byte) {
	t.Helper()
	id, s, content, h, pol, object, err := buildSample(kdf, pw, file)
	if err != nil {
		t.Fatal(err)
	}
	return id, s, content, h, pol, object
}

func TestAK8TamperedCiphertextFails(t *testing.T) {
	id, _, content, _, pol, object := sampleMessage(t, core.KDFNone, "", nil)
	for _, pos := range []int{core.HeaderLen, core.HeaderLen + 100, len(object) - 1} {
		tampered := append([]byte(nil), object...)
		tampered[pos] ^= 0x01
		if _, _, err := core.Open(&content.K, id, pol, core.KDFNone, tampered); !errors.Is(err, core.ErrIntegrity) {
			t.Fatalf("bit flip at %d: want ErrIntegrity, got %v", pos, err)
		}
	}
	tampered := append([]byte(nil), object...)
	tampered[5] ^= 0x01
	if _, _, err := core.Open(&content.K, id, pol, core.KDFNone, tampered); !errors.Is(err, core.ErrIntegrity) {
		t.Fatalf("iv flip: want ErrIntegrity, got %v", err)
	}
}

func TestAK8TamperedPolicyFails(t *testing.T) {
	id, _, content, _, pol, object := sampleMessage(t, core.KDFNone, "", nil)
	for _, bad := range []core.Policy{{OpensMax: 2, TTLSeconds: pol.TTLSeconds}, {OpensMax: pol.OpensMax, TTLSeconds: pol.TTLSeconds + 1}} {
		if _, _, err := core.Open(&content.K, id, bad, core.KDFNone, object); !errors.Is(err, core.ErrIntegrity) {
			t.Fatalf("policy %+v: want ErrIntegrity, got %v", bad, err)
		}
	}
}

func TestEnvelopeBoundToID(t *testing.T) {
	_, _, content, _, pol, object := sampleMessage(t, core.KDFNone, "", nil)
	other := core.NewID()
	if _, _, err := core.Open(&content.K, other, pol, core.KDFNone, object); !errors.Is(err, core.ErrIntegrity) {
		t.Fatalf("other id: want ErrIntegrity, got %v", err)
	}
}

func TestOpenRejectsVersionFlagsAndKDF(t *testing.T) {
	id, _, content, _, pol, object := sampleMessage(t, core.KDFNone, "", nil)
	v := append([]byte(nil), object...)
	v[0] = 2
	if _, _, err := core.Open(&content.K, id, pol, core.KDFNone, v); !errors.Is(err, core.ErrVersion) {
		t.Fatalf("version: got %v", err)
	}
	fl := append([]byte(nil), object...)
	fl[2] = 1
	if _, _, err := core.Open(&content.K, id, pol, core.KDFNone, fl); !errors.Is(err, core.ErrFlags) {
		t.Fatalf("flags: got %v", err)
	}
	if _, _, err := core.Open(&content.K, id, pol, core.KDFScrypt, object); !errors.Is(err, core.ErrKDFMismatch) {
		t.Fatalf("kdf mismatch: got %v", err)
	}
	if _, _, err := core.Open(&content.K, id, pol, core.KDFNone, object[:core.HeaderLen+3]); err == nil {
		t.Fatal("truncated object accepted")
	}
}

func TestWrongPassphraseFailsIntegrity(t *testing.T) {
	id, s, content, _, pol, object := sampleMessage(t, core.KDFPBKDF2, "right", nil)
	_, wrong, err := core.DeriveAll(id, s, core.KDFPBKDF2, "wrong")
	if err != nil {
		t.Fatal(err)
	}
	if wrong.K == content.K {
		t.Fatal("different passphrases derived the same key")
	}
	if _, _, err := core.Open(&wrong.K, id, pol, core.KDFPBKDF2, object); !errors.Is(err, core.ErrIntegrity) {
		t.Fatalf("wrong passphrase: got %v", err)
	}
}

func TestPaddingBoundaries(t *testing.T) {
	cases := []struct {
		n       int
		hasFile bool
		want    int
	}{
		{0, false, 0}, {1, false, 4096}, {4095, false, 4096}, {4096, false, 4096}, {4097, false, 8192},
		{1, true, 65536}, {65536, true, 65536}, {65537, true, 131072},
	}
	for _, c := range cases {
		if got := core.PaddedLen(c.n, c.hasFile); got != c.want {
			t.Fatalf("PaddedLen(%d,%v)=%d want %d", c.n, c.hasFile, got, c.want)
		}
	}
	for _, size := range []int{0, 1, 4095, 4096, 4097, 65536, 65537} {
		_, _, _, _, _, object := sampleMessage(t, core.KDFNone, "", make([]byte, size))
		pt := len(object) - core.HeaderLen - core.TagLen
		if pt%core.PadFile != 0 {
			t.Fatalf("file size %d: plaintext %d not a multiple of 64 KiB", size, pt)
		}
	}
	text := strings.Repeat("t", 5000)
	id := core.NewID()
	s := core.NewSecret()
	_, content, _ := core.DeriveAll(id, s, core.KDFNone, "")
	object, err := core.Seal(&content.K, core.NewHeader(core.KDFNone), id, core.Policy{OpensMax: 1, TTLSeconds: 60}, core.Payload{Meta: core.Meta{Text: &text}})
	if err != nil {
		t.Fatal(err)
	}
	if pt := len(object) - core.HeaderLen - core.TagLen; pt != 8192 {
		t.Fatalf("text plaintext %d, want 8192", pt)
	}
}

func TestSealRejectsLimits(t *testing.T) {
	id := core.NewID()
	s := core.NewSecret()
	_, content, _ := core.DeriveAll(id, s, core.KDFNone, "")
	h := core.NewHeader(core.KDFNone)
	pol := core.Policy{OpensMax: 1, TTLSeconds: 86400}
	big := make([]byte, core.MaxFileBytes+1)
	_, err := core.Seal(&content.K, h, id, pol, core.Payload{Meta: core.Meta{File: &core.FileMeta{Name: "x", Size: int64(len(big))}}, File: big})
	if !errors.Is(err, core.ErrMeta) {
		t.Fatalf("file over limit: got %v", err)
	}
	text := strings.Repeat("x", core.MaxTextBytes+1)
	if _, err := core.Seal(&content.K, h, id, pol, core.Payload{Meta: core.Meta{Text: &text}}); !errors.Is(err, core.ErrMeta) {
		t.Fatalf("text over limit: got %v", err)
	}
	mismatch := core.Payload{Meta: core.Meta{File: &core.FileMeta{Name: "x", Size: 2}}, File: []byte{1}}
	if _, err := core.Seal(&content.K, h, id, pol, mismatch); !errors.Is(err, core.ErrMeta) {
		t.Fatalf("size mismatch: got %v", err)
	}
	ok := "ok"
	for _, p := range []core.Policy{{OpensMax: 0, TTLSeconds: 60}, {OpensMax: 1, TTLSeconds: 59}} {
		if _, err := core.Seal(&content.K, h, id, p, core.Payload{Meta: core.Meta{Text: &ok}}); !errors.Is(err, core.ErrPolicy) {
			t.Fatalf("policy %+v: want ErrPolicy, got %v", p, err)
		}
	}
	if _, err := core.Seal(&content.K, core.Header{Version: 1, Flags: 1}, id, pol, core.Payload{Meta: core.Meta{Text: &text}}); !errors.Is(err, core.ErrFlags) {
		t.Fatalf("flags: got %v", err)
	}
	max := make([]byte, core.MaxFileBytes)
	maxText := strings.Repeat("y", core.MaxTextBytes)
	object, err := core.Seal(&content.K, h, id, pol, core.Payload{Meta: core.Meta{Text: &maxText, File: &core.FileMeta{Name: "max.bin", Mime: "application/octet-stream", Size: int64(len(max))}}, File: max})
	if err != nil {
		t.Fatal(err)
	}
	if len(object) > core.MaxObjectBytes {
		t.Fatalf("maximal object %d exceeds MaxObjectBytes %d", len(object), core.MaxObjectBytes)
	}
}

func TestCanonicalBase64(t *testing.T) {
	id := core.NewID()
	enc := id.String()
	idChars := len(enc)
	if idChars != 22 {
		t.Fatal("id length")
	}
	if _, err := core.ParseID(enc); err != nil {
		t.Fatal(err)
	}
	last := enc[idChars-1]
	if !strings.ContainsRune("AQgw", rune(last)) {
		t.Fatalf("canonical id ends with %q", last)
	}
	bad := enc[:idChars-1] + "B"
	if _, err := core.ParseID(bad); !errors.Is(err, core.ErrEncoding) {
		t.Fatalf("non-canonical id accepted: %v", err)
	}
	for _, s := range []string{enc + "=", enc[:21], enc + "A", "+" + enc[1:], "/" + enc[1:], enc[:21] + "\n", enc[:10] + " " + enc[11:]} {
		if _, err := core.ParseID(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	for i := 0; i < 200; i++ {
		s := core.NewSecret()
		e := core.Encode(s[:])
		if strings.HasSuffix(e, "-") || strings.HasSuffix(e, "_") {
			t.Fatalf("canonical secret ends with %q", e[len(e)-1])
		}
		if _, err := core.Decode32(e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKDFNoneIgnoresPassphrase(t *testing.T) {
	var salt [32]byte
	p, err := core.DerivePassphraseKey(core.KDFNone, "anything", &salt)
	if err != nil || len(p) != 0 {
		t.Fatalf("kdf none: %v %x", err, p)
	}
	if _, err := core.DerivePassphraseKey(core.KDF(3), "x", &salt); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("unknown kdf: %v", err)
	}
}

func TestAssertProfile(t *testing.T) {
	for _, reported := range []int{int(core.KDFPBKDF2), 3, 257, -255} {
		if err := core.AssertProfile(core.KDFScrypt, reported); !errors.Is(err, core.ErrKDFMismatch) {
			t.Fatalf("server-reported kdf_id %d accepted for a scrypt link", reported)
		}
	}
	if err := core.AssertProfile(core.KDFScrypt, int(core.KDFScrypt)); err != nil {
		t.Fatal(err)
	}
	if err := core.AssertProfile(core.KDFScrypt, int(core.KDFPBKDF2)); strings.Contains(err.Error(), "envelope") {
		t.Fatalf("a server-reported mismatch must not be reported as an envelope error: %v", err)
	}
}

func TestIllFormedPassphraseIsRejected(t *testing.T) {
	var salt [32]byte
	for _, kdf := range []core.KDF{core.KDFScrypt, core.KDFPBKDF2} {
		if _, err := core.DerivePassphraseKey(kdf, "caf\xe9", &salt); !errors.Is(err, core.ErrMalformedPassphrase) {
			t.Fatalf("kdf %s accepted invalid UTF-8: %v", kdf, err)
		}
		link, _, err := core.DeriveAll(core.NewID(), core.NewSecret(), kdf, "caf\xe9")
		if !errors.Is(err, core.ErrMalformedPassphrase) || link != (core.LinkKeys{}) {
			t.Fatalf("kdf %s: DeriveAll must refuse before deriving anything: %v", kdf, err)
		}
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	h := core.NewHeader(core.KDFScrypt)
	got, err := core.ParseHeader(h.Bytes())
	if err != nil || got != h {
		t.Fatalf("round trip: %v", err)
	}
	if !bytes.Equal(core.AAD(h, core.ID{}, core.Policy{OpensMax: 1, TTLSeconds: 60})[:core.HeaderLen], h.Bytes()) {
		t.Fatal("aad prefix")
	}
}

func TestOpenRejectsMalformedPlaintext(t *testing.T) {
	id := core.NewID()
	_, content, err := core.DeriveAll(id, core.NewSecret(), core.KDFNone, "")
	if err != nil {
		t.Fatal(err)
	}
	h := core.NewHeader(core.KDFNone)
	pol := core.Policy{OpensMax: 1, TTLSeconds: 60}
	block, _ := aes.NewCipher(content.K[:])
	gcm, _ := cipher.NewGCM(block)
	seal := func(pt []byte) []byte { return gcm.Seal(h.Bytes(), h.IV[:], pt, core.AAD(h, id, pol)) }
	withMeta := func(lenMeta uint32, meta string) []byte {
		pt := make([]byte, 4096)
		binary.BigEndian.PutUint32(pt, lenMeta)
		copy(pt[4:], meta)
		return pt
	}
	fileMeta := `{"v":1,"file":{"name":"a","mime":"","size":9999}}`
	cases := map[string][]byte{
		"len-meta-beyond-plaintext": withMeta(5000, ""),
		"len-meta-above-limit":      append(withMeta(core.MaxMetaBytes+1, ""), make([]byte, core.MaxMetaBytes+4096)...),
		"file-beyond-plaintext":     withMeta(uint32(len(fileMeta)), fileMeta),
		"too-short":                 {0, 0},
	}
	for name, pt := range cases {
		if _, _, err := core.Open(&content.K, id, pol, core.KDFNone, seal(pt)); !errors.Is(err, core.ErrPlaintext) {
			t.Fatalf("%s: want ErrPlaintext, got %v", name, err)
		}
	}
}

func TestObjectLimitFitsLargestObject(t *testing.T) {
	for _, maxFile := range []int64{1, 26621, core.PadFile, 1 << 20, core.MaxFileBytes} {
		largest := int64(core.HeaderLen + core.TagLen + core.PaddedLen(core.MetaLenPrefix+core.MaxMetaBytes+int(maxFile), true))
		if got := core.ObjectLimit(maxFile); got < largest || got > core.MaxObjectBytes {
			t.Errorf("ObjectLimit(%d) = %d, the largest valid object is %d", maxFile, got, largest)
		}
	}
	if got := core.ObjectLimit(26621); got != 196639 {
		t.Errorf("ObjectLimit(26621) = %d, want 196639", got)
	}
}

func TestKDFNoneIgnoresAnIllFormedPassphrase(t *testing.T) {
	var salt [32]byte
	if key, err := core.DerivePassphraseKey(core.KDFNone, "caf\xe9", &salt); err != nil || key != nil {
		t.Fatalf("kdf 0 must ignore the passphrase: %v %v", key, err)
	}
}

func TestRequestSignatureBindsEveryField(t *testing.T) {
	seed := core.NewSecret()
	var challenge [core.ChallengeLen]byte
	copy(challenge[:], bytes.Repeat([]byte{7}, core.ChallengeLen))
	body := []byte(`{"id":"x"}`)
	sig := core.SignRequest(seed, core.PurposeRead, "POST", "/api/v1/abuse", challenge, body)
	if !sig.Verify(core.PurposeRead, "POST", "/api/v1/abuse", body) {
		t.Fatal("own signature rejected")
	}
	other := sig
	other.Challenge[0] ^= 1
	for name, ok := range map[string]bool{
		"purpose":   sig.Verify(core.PurposeManage, "POST", "/api/v1/abuse", body),
		"method":    sig.Verify(core.PurposeRead, "GET", "/api/v1/abuse", body),
		"path":      sig.Verify(core.PurposeRead, "POST", "/api/v1/abuses", body),
		"body":      sig.Verify(core.PurposeRead, "POST", "/api/v1/abuse", []byte(`{"id":"y"}`)),
		"challenge": other.Verify(core.PurposeRead, "POST", "/api/v1/abuse", body),
	} {
		if ok {
			t.Fatalf("a signature verified with another %s", name)
		}
	}
	parsed, err := core.ParseAuthorization(sig.Authorization())
	if err != nil || parsed != sig {
		t.Fatalf("authorization round trip: %v", err)
	}
	for _, bad := range []string{"", "Bearer x", core.AuthScheme + " a.b", core.AuthScheme + " " + strings.Repeat("A", 43) + "." + strings.Repeat("A", 48) + "." + strings.Repeat("A", 85) + "B"} {
		if _, err := core.ParseAuthorization(bad); !errors.Is(err, core.ErrAuthorization) {
			t.Fatalf("%q accepted", bad)
		}
	}
	v, err := core.ParseVerifierHeader(sig.VerifierHeader(), challenge)
	if err != nil || v != sig {
		t.Fatalf("verifier header round trip: %v", err)
	}
}

func TestValidPublicKeyRejectsDegenerateKeys(t *testing.T) {
	ff := func(first, last byte) []byte {
		b := bytes.Repeat([]byte{0xff}, 32)
		b[0], b[31] = first, last
		return b
	}
	withSign := func(b []byte) []byte {
		out := append([]byte(nil), b...)
		out[31] |= 0x80
		return out
	}
	zero := make([]byte, 32)
	identity := append([]byte{1}, make([]byte, 31)...)
	order8a, _ := hex.DecodeString("26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05")
	order8b, _ := hex.DecodeString("c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a")
	smallOrder := map[string][]byte{"identity": identity, "order 2": ff(0xec, 0x7f), "order 4": zero, "order 8 a": order8a, "order 8 b": order8b}
	nonCanonical := map[string][]byte{"y = p": ff(0xed, 0x7f), "y = p + 1": ff(0xee, 0x7f), "identity with sign": withSign(identity), "order 2 with sign": withSign(ff(0xec, 0x7f))}
	for name, pk := range smallOrder {
		p, err := new(edwards25519.Point).SetBytes(pk)
		if err != nil || new(edwards25519.Point).MultByCofactor(p).Equal(edwards25519.NewIdentityPoint()) != 1 {
			t.Fatalf("%s is not a small-order point, the test data is wrong", name)
		}
		if core.ValidPublicKey(pk) {
			t.Fatalf("accepted the small-order key %s", name)
		}
	}
	for k := byte(2); k <= 18; k++ {
		pk := ff(0xed+k, 0x7f)
		if p, err := new(edwards25519.Point).SetBytes(pk); err == nil && new(edwards25519.Point).MultByCofactor(p).Equal(edwards25519.NewIdentityPoint()) == 0 {
			nonCanonical[fmt.Sprintf("y = p + %d", k)] = pk
		}
	}
	if len(nonCanonical) == 4 {
		t.Fatal("no non-canonical encoding of a point outside the small-order group, the test data is wrong")
	}
	for name, pk := range nonCanonical {
		if core.ValidPublicKey(pk) {
			t.Fatalf("accepted the non-canonical key %s", name)
		}
	}
	notOnCurve := make([]byte, 32)
	for i := byte(2); ; i++ {
		notOnCurve[0] = i
		if _, err := new(edwards25519.Point).SetBytes(notOnCurve); err != nil {
			break
		}
	}
	if core.ValidPublicKey(notOnCurve) || core.ValidPublicKey(identity[:31]) {
		t.Fatal("accepted a value that is not a point or not 32 bytes")
	}
	for range 64 {
		pk := core.PublicKey(core.NewSecret())
		if !core.ValidPublicKey(pk[:]) {
			t.Fatalf("rejected a derived key %x", pk)
		}
	}
	mixed, _ := new(edwards25519.Point).SetBytes(func() []byte { pk := core.PublicKey(core.NewSecret()); return pk[:] }())
	torsion, _ := new(edwards25519.Point).SetBytes(order8a)
	if !core.ValidPublicKey(new(edwards25519.Point).Add(mixed, torsion).Bytes()) {
		t.Fatal("rejected a key of mixed order, which no third party can forge for")
	}
}
