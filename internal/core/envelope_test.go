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

type sealed struct {
	id      core.ID
	key     [core.KeyLen]byte
	header  core.Header
	policy  core.Policy
	object  []byte
	payload core.Payload
}

func seal(tb testing.TB, text string, file []byte) sealed {
	tb.Helper()
	id := core.NewID()
	content, err := core.DeriveContent(id, core.NewSecret(), nil)
	if err != nil {
		tb.Fatal(err)
	}
	sample := sealed{
		id:      id,
		key:     content.K,
		header:  core.NewHeader(core.KDFNone),
		policy:  core.Policy{OpensMax: 1, TTLSeconds: 86400},
		payload: core.Payload{Meta: core.Meta{Text: &text}},
	}
	if file != nil {
		sample.payload.Meta.File = &core.FileMeta{
			Name: "f.bin",
			Mime: "application/octet-stream",
			Size: int64(len(file)),
		}
		sample.payload.File = file
	}
	sample.object, err = core.Seal(&sample.key, sample.header, id, sample.policy, sample.payload)
	if err != nil {
		tb.Fatal(err)
	}
	return sample
}

func (s *sealed) gcm(tb testing.TB) cipher.AEAD {
	tb.Helper()
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		tb.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		tb.Fatal(err)
	}
	return aead
}

func (s *sealed) sealPlaintext(aead cipher.AEAD, plaintext []byte) []byte {
	return aead.Seal(s.header.Bytes(), s.header.IV[:], plaintext, core.AAD(s.header, s.id, s.policy))
}

func flipped(object []byte, position int) []byte {
	tampered := bytes.Clone(object)
	tampered[position] ^= 0x01
	return tampered
}

func TestAK8TamperingFailsIntegrity(t *testing.T) {
	t.Parallel()
	sample := seal(t, "integrity", nil)
	otherKey, err := core.DeriveContent(sample.id, core.NewSecret(), bytes.Repeat([]byte{1}, core.KeyLen))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		key    [core.KeyLen]byte
		id     core.ID
		policy core.Policy
		object []byte
	}{
		"first ciphertext bit": {sample.key, sample.id, sample.policy, flipped(sample.object, core.HeaderLen)},
		"inner ciphertext bit": {sample.key, sample.id, sample.policy, flipped(sample.object, core.HeaderLen+100)},
		"tag bit":              {sample.key, sample.id, sample.policy, flipped(sample.object, len(sample.object)-1)},
		"iv bit":               {sample.key, sample.id, sample.policy, flipped(sample.object, 5)},
		"opens_max":            {sample.key, sample.id, core.Policy{OpensMax: 2, TTLSeconds: 86400}, sample.object},
		"ttl_s":                {sample.key, sample.id, core.Policy{OpensMax: 1, TTLSeconds: 86401}, sample.object},
		"another id":           {sample.key, core.NewID(), sample.policy, sample.object},
		"another key":          {otherKey.K, sample.id, sample.policy, sample.object},
	}
	for name, tampered := range cases {
		_, _, openErr := core.Open(&tampered.key, tampered.id, tampered.policy, core.KDFNone, tampered.object)
		if !errors.Is(openErr, core.ErrIntegrity) {
			t.Errorf("%s: want ErrIntegrity, got %v", name, openErr)
		}
	}
}

func TestEveryHeaderGetsAFreshRandomIV(t *testing.T) {
	t.Parallel()
	const samples = 4096
	seen := make(map[[core.IVLen]byte]bool, samples)
	var taken [core.IVLen][256]bool
	var distinct [core.IVLen]int
	for range samples {
		iv := core.NewHeader(core.KDFNone).IV
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
	version, flags := bytes.Clone(sample.object), bytes.Clone(sample.object)
	version[0], flags[2] = 2, 1
	for name, test := range map[string]struct {
		kdf    core.KDF
		object []byte
		want   error
	}{
		"version":   {core.KDFNone, version, core.ErrVersion},
		"flags":     {core.KDFNone, flags, core.ErrFlags},
		"kdf":       {core.KDFScrypt, sample.object, core.ErrKDFMismatch},
		"truncated": {core.KDFNone, sample.object[:core.HeaderLen+3], core.ErrIntegrity},
	} {
		_, _, err := core.Open(&sample.key, sample.id, sample.policy, test.kdf, test.object)
		if !errors.Is(err, test.want) {
			t.Errorf("%s: want %v, got %v", name, test.want, err)
		}
	}
}

func TestSealEnforcesTheLimits(t *testing.T) {
	t.Parallel()
	sample := seal(t, "limits", nil)
	overText := strings.Repeat("x", core.MaxTextBytes+1)
	overFile := make([]byte, core.MaxFileBytes+1)
	for name, test := range map[string]struct {
		header  core.Header
		policy  core.Policy
		payload core.Payload
		want    error
	}{
		"file over the limit": {sample.header, sample.policy, core.Payload{
			Meta: core.Meta{File: &core.FileMeta{Name: "x", Size: int64(len(overFile))}},
			File: overFile,
		}, core.ErrMeta},
		"text over the limit": {sample.header, sample.policy, core.Payload{Meta: core.Meta{Text: &overText}}, core.ErrMeta},
		"size mismatch": {sample.header, sample.policy, core.Payload{
			Meta: core.Meta{File: &core.FileMeta{Name: "x", Size: 2}},
			File: []byte{1},
		}, core.ErrMeta},
		"no opens":     {sample.header, core.Policy{OpensMax: 0, TTLSeconds: 60}, sample.payload, core.ErrPolicy},
		"ttl under 60": {sample.header, core.Policy{OpensMax: 1, TTLSeconds: 59}, sample.payload, core.ErrPolicy},
		"flags":        {core.Header{Version: core.Version, Flags: 1}, sample.policy, sample.payload, core.ErrFlags},
	} {
		_, err := core.Seal(&sample.key, test.header, sample.id, test.policy, test.payload)
		if !errors.Is(err, test.want) {
			t.Errorf("%s: want %v, got %v", name, test.want, err)
		}
	}
	largest := seal(t, strings.Repeat("y", core.MaxTextBytes), make([]byte, core.MaxFileBytes))
	if len(largest.object) > core.MaxObjectBytes || int64(len(largest.object)) > core.ObjectLimit(core.MaxFileBytes) {
		t.Fatalf("the largest valid object has %d bytes, above the limits", len(largest.object))
	}
	for maxFileBytes, want := range map[int64]int64{26621: 196639, core.MaxFileBytes: 5373983} {
		if got := core.ObjectLimit(maxFileBytes); got != want {
			t.Errorf("ObjectLimit(%d) = %d, want %d", maxFileBytes, got, want)
		}
	}
}

func TestOpenRejectsMalformedPlaintext(t *testing.T) {
	t.Parallel()
	sample := seal(t, "plaintext", nil)
	aead := sample.gcm(t)
	withMeta := func(metaLength uint32, meta string) []byte {
		plaintext := make([]byte, core.PadText)
		binary.BigEndian.PutUint32(plaintext, metaLength)
		copy(plaintext[core.MetaLenPrefix:], meta)
		return plaintext
	}
	fileMeta := `{"v":1,"file":{"name":"a","mime":"","size":9999}}`
	for name, plaintext := range map[string][]byte{
		"meta length beyond the plaintext": withMeta(5000, ""),
		"meta length above the limit": append(
			withMeta(core.MaxMetaBytes+1, ""),
			make([]byte, core.MaxMetaBytes+core.PadText)...,
		),
		"file beyond the plaintext": withMeta(uint32(len(fileMeta)), fileMeta),
		"too short":                 {0, 0},
	} {
		object := sample.sealPlaintext(aead, plaintext)
		_, _, err := core.Open(&sample.key, sample.id, sample.policy, core.KDFNone, object)
		if !errors.Is(err, core.ErrPlaintext) {
			t.Errorf("%s: want ErrPlaintext, got %v", name, err)
		}
	}
}

func FuzzOpen(f *testing.F) {
	sample := seal(f, "fuzz", []byte{1, 2, 3})
	f.Add(sample.object)
	f.Add(sample.object[:core.HeaderLen])
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, object []byte) {
		header, payload, err := core.Open(&sample.key, sample.id, sample.policy, core.KDFNone, object)
		if err != nil {
			return
		}
		if header.Version != core.Version ||
			(payload.Meta.File != nil && int64(len(payload.File)) != payload.Meta.File.Size) {
			t.Fatalf("accepted an inconsistent envelope: %+v", header)
		}
	})
}

func FuzzOpenPlaintext(f *testing.F) {
	sample := seal(f, "hello", nil)
	aead := sample.gcm(f)
	plaintext, err := aead.Open(nil, sample.header.IV[:], sample.object[core.HeaderLen:],
		core.AAD(sample.header, sample.id, sample.policy))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(plaintext)
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, plaintext []byte) {
		object := sample.sealPlaintext(aead, plaintext)
		_, payload, openErr := core.Open(&sample.key, sample.id, sample.policy, core.KDFNone, object)
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
		meta, err := core.DecodeMeta(data)
		if err != nil {
			return
		}
		canonical, err := core.EncodeMeta(meta)
		if err != nil {
			t.Fatalf("decoded meta must encode: %v", err)
		}
		again, err := core.DecodeMeta(canonical)
		if err != nil {
			t.Fatalf("canonical meta must decode: %v", err)
		}
		if encoded, _ := core.EncodeMeta(again); !bytes.Equal(encoded, canonical) {
			t.Fatal("the canonical form is not a fixed point")
		}
	})
}
