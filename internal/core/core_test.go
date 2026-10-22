package core_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

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

func TestIllFormedPassphraseIsRejected(t *testing.T) {
	var salt [32]byte
	for _, kdf := range []core.KDF{core.KDFScrypt, core.KDFPBKDF2} {
		if _, err := core.DerivePassphraseKey(kdf, "caf\xe9", &salt); !errors.Is(err, core.ErrPassphrase) {
			t.Fatalf("kdf %s accepted invalid UTF-8: %v", kdf, err)
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
