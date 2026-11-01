package envelope_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
)

type sealed struct {
	id      core.ID
	content keys.ContentKeys
	header  envelope.Header
	policy  envelope.Policy
	object  []byte
	payload envelope.Payload
}

func contentKeys(tb testing.TB, id core.ID, passphraseKey []byte) keys.ContentKeys {
	tb.Helper()
	content, err := keys.DeriveContent(id, core.NewSecret(), passphraseKey, core.NewSecret())
	if err != nil {
		tb.Fatal(err)
	}
	return content
}

func seal(tb testing.TB, text string, file []byte) sealed {
	tb.Helper()
	id := core.NewID()
	sample := sealed{
		id:      id,
		content: contentKeys(tb, id, nil),
		header:  envelope.NewHeader(core.KDFNone),
		policy:  envelope.Policy{OpensMax: 1, TTLSeconds: 86400},
		payload: envelope.Payload{Meta: envelope.Meta{Text: &text}},
	}
	if file != nil {
		sample.payload.Meta.File = &envelope.FileMeta{
			Name: "f.bin",
			Mime: "application/octet-stream",
			Size: int64(len(file)),
		}
		sample.payload.File = file
	}
	var err error
	sample.object, err = envelope.Seal(&sample.content, sample.header, id, sample.policy, sample.payload)
	if err != nil {
		tb.Fatal(err)
	}
	return sample
}

func (s *sealed) expected(object []byte) *envelope.Expected {
	hash := envelope.ObjectHash(object)
	return &envelope.Expected{ID: s.id, Policy: s.policy, KDF: core.KDFNone, ObjectHash: &hash}
}

func (s *sealed) open(object []byte) (envelope.Header, envelope.Payload, error) {
	return envelope.Open(&s.content, s.expected(object), object)
}

func (s *sealed) aead(tb testing.TB) cipher.AEAD {
	tb.Helper()
	block, err := aes.NewCipher(s.content.K[:])
	if err != nil {
		tb.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		tb.Fatal(err)
	}
	return aead
}

// sealPlaintext pads any plaintext to whole blocks and seals it with a valid commitment, so a test reaches the plaintext checks.
func (s *sealed) sealPlaintext(aead cipher.AEAD, plaintext []byte) []byte {
	overhead := envelope.PrefixLen + envelope.TagLen
	blocks := (len(plaintext) + overhead + envelope.Pad - 1) / envelope.Pad
	padded := make([]byte, blocks*envelope.Pad-overhead)
	copy(padded, plaintext)
	commitment := envelope.Commitment(&s.content.Commitment, s.header, s.id, s.policy)
	prefix := append(s.header.Bytes(), commitment[:]...)
	return aead.Seal(prefix, s.header.IV[:], padded, envelope.AAD(s.header, commitment[:], s.id, s.policy))
}

func (*sealed) plaintextLen() int {
	return envelope.Pad - envelope.PrefixLen - envelope.TagLen
}

func flipped(object []byte, position int) []byte {
	tampered := bytes.Clone(object)
	tampered[position] ^= 0x01
	return tampered
}

func TestAK8TamperingFailsIntegrity(t *testing.T) {
	t.Parallel()
	sample := seal(t, "integrity", nil)
	prefix := envelope.PrefixLen
	otherKeys := contentKeys(t, sample.id, bytes.Repeat([]byte{1}, core.KeyLen))
	cases := map[string]struct {
		content keys.ContentKeys
		id      core.ID
		policy  envelope.Policy
		object  []byte
	}{
		"first ciphertext bit": {sample.content, sample.id, sample.policy, flipped(sample.object, prefix)},
		"inner ciphertext bit": {sample.content, sample.id, sample.policy, flipped(sample.object, prefix+100)},
		"tag bit": {
			sample.content,
			sample.id,
			sample.policy,
			flipped(sample.object, len(sample.object)-1),
		},
		"iv bit":         {sample.content, sample.id, sample.policy, flipped(sample.object, 5)},
		"commitment bit": {sample.content, sample.id, sample.policy, flipped(sample.object, envelope.HeaderLen)},
		"opens_max": {
			sample.content,
			sample.id,
			envelope.Policy{OpensMax: 2, TTLSeconds: 86400},
			sample.object,
		},
		"ttl_s": {
			sample.content,
			sample.id,
			envelope.Policy{OpensMax: 1, TTLSeconds: 86401},
			sample.object,
		},
		"another id":  {sample.content, core.NewID(), sample.policy, sample.object},
		"another key": {otherKeys, sample.id, sample.policy, sample.object},
	}
	for name, tampered := range cases {
		hash := envelope.ObjectHash(tampered.object)
		expected := envelope.Expected{
			ID:         tampered.id,
			Policy:     tampered.policy,
			KDF:        core.KDFNone,
			ObjectHash: &hash,
		}
		_, _, openErr := envelope.Open(&tampered.content, &expected, tampered.object)
		if !errors.Is(openErr, envelope.ErrIntegrity) {
			t.Errorf("%s: want ErrIntegrity, got %v", name, openErr)
		}
	}
}

func TestF1ReaderRejectsAnObjectTheLinkDoesNotPin(t *testing.T) {
	t.Parallel()
	sample := seal(t, "the original", nil)
	forged, err := envelope.Seal(&sample.content, envelope.NewHeader(core.KDFNone), sample.id, sample.policy,
		envelope.Payload{Meta: envelope.Meta{Text: new("a forgery under the same keys")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = sample.open(forged); err != nil {
		t.Fatalf("the forgery authenticates under the keys of the link: %v", err)
	}
	if _, _, err = envelope.Open(
		&sample.content,
		sample.expected(sample.object),
		forged,
	); !errors.Is(
		err,
		envelope.ErrIntegrity,
	) {
		t.Fatalf("the reader opened an object other than the one its link pins: %v", err)
	}
	expected := sample.expected(sample.object)
	expected.ObjectHash = nil
	if _, _, err = envelope.Open(&sample.content, expected, sample.object); !errors.Is(err, envelope.ErrIntegrity) {
		t.Fatalf("a reader without a hash opened the object: %v", err)
	}
}

func TestF4CommitmentRejectsAnotherKeyBeforeDecrypting(t *testing.T) {
	t.Parallel()
	sample := seal(t, "commitment", nil)
	other := sample.content
	other.Commitment[0] ^= 1
	_, _, err := envelope.Open(&other, sample.expected(sample.object), sample.object)
	if !errors.Is(err, envelope.ErrIntegrity) {
		t.Fatalf("a commitment key that did not make the object passed: %v", err)
	}
}

func TestEveryHeaderGetsAFreshRandomIV(t *testing.T) {
	t.Parallel()
	const samples = 4096
	seen := make(map[[envelope.IVLen]byte]bool, samples)
	var taken [envelope.IVLen][256]bool
	var distinct [envelope.IVLen]int
	for range samples {
		iv := envelope.NewHeader(core.KDFNone).IV
		if seen[iv] {
			t.Fatalf("IV %x repeated within %d headers", iv, samples)
		}
		seen[iv] = true
		for i, octet := range iv {
			if !taken[i][octet] {
				taken[i][octet] = true
				distinct[i]++
			}
		}
	}
	for i, count := range distinct {
		if count < 200 {
			t.Errorf("byte %d of the IV took %d of 256 values in %d headers", i, count, samples)
		}
	}
}

func TestOpenRejectsAnUnsupportedHeader(t *testing.T) {
	t.Parallel()
	sample := seal(t, "header", nil)
	version, flags, released := bytes.Clone(sample.object), bytes.Clone(sample.object), bytes.Clone(sample.object)
	version[0], flags[2], released[0] = 3, 1, 1
	for name, test := range map[string]struct {
		kdf    core.KDF
		object []byte
		want   error
	}{
		"version":         {core.KDFNone, version, envelope.ErrVersion},
		"version 1":       {core.KDFNone, released, envelope.ErrVersion},
		"flags":           {core.KDFNone, flags, envelope.ErrFlags},
		"kdf":             {core.KDFScrypt, sample.object, core.ErrKDFMismatch},
		"truncated":       {core.KDFNone, sample.object[:envelope.PrefixLen+3], envelope.ErrIntegrity},
		"not 64 KiB long": {core.KDFNone, sample.object[:len(sample.object)-1], envelope.ErrIntegrity},
	} {
		expected := sample.expected(test.object)
		expected.KDF = test.kdf
		_, _, err := envelope.Open(&sample.content, expected, test.object)
		if !errors.Is(err, test.want) {
			t.Errorf("%s: want %v, got %v", name, test.want, err)
		}
	}
}

func TestF7TextAndFilePadToTheSameLength(t *testing.T) {
	t.Parallel()
	text := seal(t, strings.Repeat("t", 3000), nil)
	file := seal(t, "", bytes.Repeat([]byte{7}, 3000))
	if len(text.object) != envelope.Pad || len(file.object) != envelope.Pad {
		t.Fatalf("a text of %d and a file of %d bytes, want one 64 KiB block each", len(text.object), len(file.object))
	}
}

func TestSealEnforcesTheLimits(t *testing.T) {
	t.Parallel()
	sample := seal(t, "limits", nil)
	overText := strings.Repeat("x", envelope.MaxTextBytes+1)
	overFile := make([]byte, envelope.MaxFileBytes+1)
	for name, test := range map[string]struct {
		header  envelope.Header
		policy  envelope.Policy
		payload envelope.Payload
		want    error
	}{
		"file over the limit": {sample.header, sample.policy, envelope.Payload{
			Meta: envelope.Meta{File: &envelope.FileMeta{Name: "x", Size: int64(len(overFile))}},
			File: overFile,
		}, envelope.ErrMeta},
		"text over the limit": {sample.header, sample.policy, envelope.Payload{Meta: envelope.Meta{Text: &overText}}, envelope.ErrMeta},
		"size mismatch": {sample.header, sample.policy, envelope.Payload{
			Meta: envelope.Meta{File: &envelope.FileMeta{Name: "x", Size: 2}},
			File: []byte{1},
		}, envelope.ErrMeta},
		"no opens":     {sample.header, envelope.Policy{OpensMax: 0, TTLSeconds: 60}, sample.payload, envelope.ErrPolicy},
		"ttl under 60": {sample.header, envelope.Policy{OpensMax: 1, TTLSeconds: 59}, sample.payload, envelope.ErrPolicy},
		"flags":        {envelope.Header{Version: envelope.Version, Flags: 1}, sample.policy, sample.payload, envelope.ErrFlags},
	} {
		_, err := envelope.Seal(&sample.content, test.header, sample.id, test.policy, test.payload)
		if !errors.Is(err, test.want) {
			t.Errorf("%s: want %v, got %v", name, test.want, err)
		}
	}
	largest := seal(t, strings.Repeat("y", envelope.MaxTextBytes), make([]byte, envelope.MaxFileBytes))
	if len(largest.object) > envelope.MaxObjectBytes ||
		int64(len(largest.object)) > envelope.ObjectLimit(envelope.MaxFileBytes) {
		t.Fatalf("the largest valid object has %d bytes, above the limits", len(largest.object))
	}
	for maxFileBytes, want := range map[int64]int64{26557: 131072, 26558: 196608, envelope.MaxFileBytes: 5373952} {
		if got := envelope.ObjectLimit(maxFileBytes); got != want {
			t.Errorf("ObjectLimit(%d) = %d, want %d", maxFileBytes, got, want)
		}
	}
}

func TestOpenRejectsMalformedPlaintext(t *testing.T) {
	t.Parallel()
	sample := seal(t, "plaintext", nil)
	aead := sample.aead(t)
	withMeta := func(metaLength uint32, meta string) []byte {
		plaintext := make([]byte, sample.plaintextLen())
		binary.BigEndian.PutUint32(plaintext, metaLength)
		copy(plaintext[envelope.MetaLenPrefix:], meta)
		return plaintext
	}
	fileMeta := `{"v":1,"file":{"name":"a","mime":"","size":99999}}`
	for name, plaintext := range map[string][]byte{
		"meta length beyond the plaintext": withMeta(uint32(sample.plaintextLen()), ""),
		"meta length above the limit": append(
			withMeta(envelope.MaxMetaBytes+1, ""),
			make([]byte, 2*envelope.Pad)...,
		),
		"file beyond the plaintext": withMeta(uint32(len(fileMeta)), fileMeta),
	} {
		object := sample.sealPlaintext(aead, plaintext)
		_, _, err := sample.open(object)
		if !errors.Is(err, envelope.ErrPlaintext) {
			t.Errorf("%s: want ErrPlaintext, got %v", name, err)
		}
	}
}

func FuzzOpen(f *testing.F) {
	sample := seal(f, "fuzz", []byte{1, 2, 3})
	f.Add(sample.object)
	f.Add(sample.object[:envelope.HeaderLen])
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, object []byte) {
		header, payload, err := sample.open(object)
		if err != nil {
			return
		}
		if header.Version != envelope.Version ||
			(payload.Meta.File != nil && int64(len(payload.File)) != payload.Meta.File.Size) {
			t.Fatalf("accepted an inconsistent envelope: %+v", header)
		}
	})
}

func FuzzOpenPlaintext(f *testing.F) {
	sample := seal(f, "hello", nil)
	aead := sample.aead(f)
	prefix := envelope.PrefixLen
	plaintext, err := aead.Open(nil, sample.header.IV[:], sample.object[prefix:],
		envelope.AAD(sample.header, sample.object[envelope.HeaderLen:prefix], sample.id, sample.policy))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(plaintext)
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Fuzz(func(t *testing.T, content []byte) {
		padded := make([]byte, max(sample.plaintextLen(), len(content)))
		copy(padded, content)
		object := sample.sealPlaintext(aead, padded)
		_, payload, openErr := sample.open(object)
		if openErr == nil && payload.Meta.File != nil && int64(len(payload.File)) != payload.Meta.File.Size {
			t.Fatal("the file length differs from meta.file.size")
		}
	})
}

func FuzzDecodeMeta(f *testing.F) {
	f.Add([]byte(`{"v":1,"text":"x"}`))
	f.Add([]byte(`{"v":1,"file":{"name":"a","mime":"b","size":1}}`))
	f.Add([]byte(`{"v":1`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		meta, err := envelope.DecodeMeta(data)
		if err != nil {
			return
		}
		canonical, err := envelope.EncodeMeta(meta)
		if err != nil {
			t.Fatalf("decoded meta must encode: %v", err)
		}
		again, err := envelope.DecodeMeta(canonical)
		if err != nil {
			t.Fatalf("canonical meta must decode: %v", err)
		}
		if encoded, _ := envelope.EncodeMeta(again); !bytes.Equal(encoded, canonical) {
			t.Fatal("the canonical form is not a fixed point")
		}
	})
}
