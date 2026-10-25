package core_test

import (
	"crypto/aes"
	"crypto/cipher"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func FuzzOpenPlaintext(f *testing.F) {
	id := core.NewID()
	_, content, err := core.DeriveAll(id, core.NewSecret(), core.KDFNone, "")
	if err != nil {
		f.Fatal(err)
	}
	h := core.NewHeader(core.KDFNone)
	pol := core.Policy{OpensMax: 1, TTLSeconds: 60}
	block, err := aes.NewCipher(content.K[:])
	if err != nil {
		f.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		f.Fatal(err)
	}
	text := "hello"
	valid, err := core.Seal(&content.K, h, id, pol, core.Payload{Meta: core.Meta{Text: &text}})
	if err != nil {
		f.Fatal(err)
	}
	plain, err := gcm.Open(nil, h.IV[:], valid[core.HeaderLen:], core.AAD(h, id, pol))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(plain)
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, pt []byte) {
		_, payload, err := core.Open(
			&content.K,
			id,
			pol,
			core.KDFNone,
			gcm.Seal(h.Bytes(), h.IV[:], pt, core.AAD(h, id, pol)),
		)
		if err != nil {
			return
		}
		if payload.Meta.File != nil && int64(len(payload.File)) != payload.Meta.File.Size {
			t.Fatal("file size mismatch")
		}
	})
}

func FuzzOpen(f *testing.F) {
	id, _, content, _, pol, object, err := buildSample(core.KDFNone, "", []byte{1, 2, 3})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(object)
	f.Add(object[:core.HeaderLen])
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		h, payload, err := core.Open(&content.K, id, pol, core.KDFNone, data)
		if err != nil {
			return
		}
		if h.Version != core.Version {
			t.Fatal("accepted unknown version")
		}
		if payload.Meta.File != nil && int64(len(payload.File)) != payload.Meta.File.Size {
			t.Fatal("file size mismatch")
		}
	})
}

func FuzzDecodeMeta(f *testing.F) {
	f.Add([]byte(`{"v":1,"text":"x"}`))
	f.Add([]byte(`{"v":1,"file":{"name":"a","mime":"b","size":1}}`))
	f.Add([]byte(`{"v":1`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := core.DecodeMeta(data)
		if err != nil {
			return
		}
		can, err := core.EncodeMeta(m)
		if err != nil {
			t.Fatalf("decoded meta must encode: %v", err)
		}
		again, err := core.DecodeMeta(can)
		if err != nil {
			t.Fatalf("canonical meta must decode: %v", err)
		}
		can2, _ := core.EncodeMeta(again)
		if string(can2) != string(can) {
			t.Fatal("canonical form not stable")
		}
	})
}

func FuzzParseLink(f *testing.F) {
	id := core.NewID()
	s := core.NewSecret()
	f.Add(core.FormatLink("https://nylzo.example", id, core.KDFScrypt, s))
	f.Add("https://nylzo.example/m/" + id.String())
	f.Add("#1.0.")
	f.Fuzz(func(t *testing.T, raw string) {
		link, err := core.ParseLink(raw)
		if err != nil {
			return
		}
		secret := link.Secret
		again, err := core.ParseLink(core.FormatLink("https://nylzo.example", link.ID, link.KDF, &secret))
		if err != nil || again != link {
			t.Fatal("parsed link does not round trip")
		}
	})
}

func FuzzParseFragment(f *testing.F) {
	s := core.NewSecret()
	f.Add(core.Fragment(core.KDFNone, s))
	f.Add("1.2." + core.Encode(s[:]))
	f.Fuzz(func(t *testing.T, frag string) {
		kdf, secret, err := core.ParseFragment(frag)
		if err != nil {
			return
		}
		if !kdf.Valid() || secret == nil {
			t.Fatal("invalid parse result")
		}
	})
}
