package core_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"golang.org/x/crypto/scrypt"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/vectors"
)

func loadVectors(t *testing.T) *vectors.File {
	t.Helper()
	data, err := os.ReadFile("../../spec/vectors.json")
	if err != nil {
		t.Fatalf("spec/vectors.json: %v", err)
	}
	var f vectors.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return &f
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func to32(t *testing.T, b []byte) *[32]byte {
	t.Helper()
	if len(b) != 32 {
		t.Fatalf("want 32 bytes, got %d", len(b))
	}
	var out [32]byte
	copy(out[:], b)
	return &out
}

var rfc7914 = map[string]string{
	"rfc7914-1": "77d6576238657b203b19ca42c18a0497f16b4844e3074ae8dfdffa3fede21442fcd0069ded0948f8326a753a0fc81f17e8d3e0fb2e0d3628cf35e20c38d18906",
	"rfc7914-2": "fdbabe1c9d3472007856e7190d01e9fe7c6ad7cbc8237830e77376634b3731622eaf30d92e22a3886ff109279d9830dac727afb94a83ee6d8360cbdfa2cc0640",
	"rfc7914-3": "7023bdcb3afd7348461c06cd81fd38ebfda8fbba904f8e3ea9b543f6545da1f2d5432955613f0fcf62d49705242a9af9e61e85dc0d651e40dfcf017b45575887",
	"rfc7914-4": "2101cb9b6a511aaeaddbbe09cf70f881ec568d574a2ffd4dabe5ee9820adaa478e56fd8f4ba5d09ffa1c6d927c40f4c337304049e8a952fbcbf45c6fa77a41a4",
}

func TestScryptRFC7914(t *testing.T) {
	f := loadVectors(t)
	if len(f.Scrypt) != 4 {
		t.Fatalf("want 4 RFC 7914 vectors, got %d", len(f.Scrypt))
	}
	for _, v := range f.Scrypt {
		want, ok := rfc7914[v.Name]
		if !ok {
			t.Fatalf("unknown scrypt vector %q", v.Name)
		}
		if v.DK != want {
			t.Fatalf("%s: vectors.json dk %s differs from RFC 7914 %s", v.Name, v.DK, want)
		}
		if v.Heavy && testing.Short() {
			continue
		}
		dk, err := scrypt.Key([]byte(v.Password), []byte(v.Salt), v.N, v.R, v.P, v.DKLen)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(dk) != want {
			t.Fatalf("%s: scrypt output differs from RFC 7914", v.Name)
		}
	}
}

func TestDeriveVectors(t *testing.T) {
	f := loadVectors(t)
	for _, v := range f.Derive {
		t.Run(v.Name, func(t *testing.T) {
			id, err := core.ParseID(v.ID)
			if err != nil {
				t.Fatal(err)
			}
			s := to32(t, mustHex(t, v.S))
			hs := core.HSalt(id)
			if hex.EncodeToString(hs[:]) != v.HSalt {
				t.Fatal("hsalt mismatch")
			}
			link, err := core.DeriveLink(id, s)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(link.R[:]) != v.R || hex.EncodeToString(link.SaltPW[:]) != v.SaltPW {
				t.Fatal("link keys mismatch")
			}
			pw := ""
			if v.Passphrase != nil {
				pw = *v.Passphrase
			}
			p, err := core.DerivePassphraseKey(core.KDF(v.KDFID), pw, &link.SaltPW)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(p) != v.P {
				t.Fatal("passphrase key mismatch")
			}
			content, err := core.DeriveContent(id, s, p)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(content.K[:]) != v.K || hex.EncodeToString(content.V[:]) != v.V {
				t.Fatal("content keys mismatch")
			}
			readKey := core.PublicKey(&link.R)
			manageKey := core.PublicKey(to32(t, mustHex(t, v.M)))
			if core.Encode(readKey[:]) != v.ReadKey || core.Encode(manageKey[:]) != v.ManageKey {
				t.Fatal("public keys mismatch")
			}
			verifierKey := core.PublicKey(&content.V)
			if (v.KDFID != 0) != (v.VerifierKey != nil) ||
				(v.VerifierKey != nil && core.Encode(verifierKey[:]) != *v.VerifierKey) {
				t.Fatal("verifier key mismatch")
			}
			if core.Fragment(core.KDF(v.KDFID), s) != v.Fragment {
				t.Fatal("fragment mismatch")
			}
		})
	}
}

func TestNFCPassphrasesAgree(t *testing.T) {
	f := loadVectors(t)
	var composed, decomposed *vectors.Derive
	for i := range f.Derive {
		switch f.Derive[i].Name {
		case "scrypt-nfc-composed":
			composed = &f.Derive[i]
		case "scrypt-nfc-decomposed":
			decomposed = &f.Derive[i]
		}
	}
	if composed == nil || decomposed == nil {
		t.Fatal("nfc vectors missing")
	}
	if *composed.Passphrase == *decomposed.Passphrase {
		t.Fatal("nfc vectors must use different Unicode forms")
	}
	if composed.ID != decomposed.ID || composed.S != decomposed.S || composed.P != decomposed.P ||
		composed.K != decomposed.K {
		t.Fatal("NFC forms must derive identical keys")
	}
}

func TestRedactionVectorsMatchTheCore(t *testing.T) {
	f := loadVectors(t)
	if len(f.Redact) == 0 {
		t.Fatal("redaction vectors missing")
	}
	for _, v := range f.Redact {
		if got := core.RedactReason(v.Reason); got != v.Redacted {
			t.Fatalf("%s: %q, want %q", v.Name, got, v.Redacted)
		}
	}
}

func fileBytes(t *testing.T, fc *vectors.FileContent) []byte {
	t.Helper()
	if fc == nil {
		return nil
	}
	if fc.Content == "counter" {
		b := make([]byte, fc.Size)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}
	b := mustHex(t, fc.Content)
	if len(b) != fc.Size {
		t.Fatalf("file content length %d differs from size %d", len(b), fc.Size)
	}
	return b
}

func TestEnvelopeVectors(t *testing.T) {
	f := loadVectors(t)
	for _, v := range f.Envelope {
		t.Run(v.Name, func(t *testing.T) {
			id, err := core.ParseID(v.ID)
			if err != nil {
				t.Fatal(err)
			}
			s := to32(t, mustHex(t, v.S))
			pw := ""
			if v.Passphrase != nil {
				pw = *v.Passphrase
			}
			_, content, err := core.DeriveAll(id, s, core.KDF(v.KDFID), pw)
			if err != nil {
				t.Fatal(err)
			}
			h := core.Header{Version: core.Version, KDF: core.KDF(v.KDFID), Flags: uint8(v.Flags)}
			copy(h.IV[:], mustHex(t, v.IV))
			pol := core.Policy{OpensMax: uint16(v.OpensMax), TTLSeconds: uint32(v.TTLSeconds)}
			payload := core.Payload{Meta: core.Meta{Text: v.Text}, File: fileBytes(t, v.File)}
			if v.File != nil {
				payload.Meta.File = &core.FileMeta{Name: v.File.Name, Mime: v.File.Mime, Size: int64(v.File.Size)}
			}
			meta, err := core.EncodeMeta(payload.Meta)
			if err != nil {
				t.Fatal(err)
			}
			if string(meta) != v.MetaJSON {
				t.Fatalf("meta mismatch:\n got %s\nwant %s", meta, v.MetaJSON)
			}
			if hex.EncodeToString(core.AAD(h, id, pol)) != v.AAD {
				t.Fatal("aad mismatch")
			}
			object, err := core.Seal(&content.K, h, id, pol, payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(object) != v.ObjectLen || len(object)-core.HeaderLen-core.TagLen != v.PlaintextLen {
				t.Fatalf("object length %d, want %d", len(object), v.ObjectLen)
			}
			sum := sha256.Sum256(object)
			if hex.EncodeToString(sum[:]) != v.ObjectSHA256 {
				t.Fatal("object sha256 mismatch")
			}
			if v.Object != nil && core.Encode(object) != *v.Object {
				t.Fatal("object bytes mismatch")
			}
			gotHeader, got, err := core.Open(&content.K, id, pol, core.KDF(v.KDFID), object)
			if err != nil {
				t.Fatal(err)
			}
			if gotHeader != h {
				t.Fatal("header mismatch after open")
			}
			if (got.Meta.Text == nil) != (v.Text == nil) || (v.Text != nil && *got.Meta.Text != *v.Text) {
				t.Fatal("text mismatch after open")
			}
			if (got.Meta.File == nil) != (v.File == nil) {
				t.Fatal("file presence mismatch after open")
			}
			if v.File != nil {
				if got.Meta.File.Name != v.File.Name || got.Meta.File.Mime != v.File.Mime ||
					got.Meta.File.Size != int64(v.File.Size) {
					t.Fatal("file meta mismatch after open")
				}
				if string(got.File) != string(payload.File) {
					t.Fatal("file bytes mismatch after open")
				}
			}
		})
	}
}

func TestLinkVectors(t *testing.T) {
	f := loadVectors(t)
	canonical := 0
	for _, v := range f.Link {
		if v.Canonical {
			canonical++
		}
	}
	if canonical == 0 {
		t.Fatal("no canonical link vectors: the format round trip would never run")
	}
	for _, v := range f.Link {
		t.Run(v.Name, func(t *testing.T) {
			link, err := core.ParseLink(v.Input)
			if !v.Valid {
				if !errors.Is(err, core.ErrLink) {
					t.Fatalf("expected rejection of %q, got %v", v.Input, err)
				}
				if errors.Is(err, core.ErrLinkVersion) != v.Newer {
					t.Fatalf("%q: newer version %v, got %v", v.Input, v.Newer, err)
				}
				if errors.Is(err, core.ErrLinkRetiredKDF) != v.RetiredKDF {
					t.Fatalf("%q: retired passphrase profile %v, got %v", v.Input, v.RetiredKDF, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if link.ID.String() != *v.ID || int(link.KDF) != *v.KDFID || hex.EncodeToString(link.Secret[:]) != *v.S {
				t.Fatal("parsed link mismatch")
			}
			s := link.Secret
			if v.Canonical && core.FormatLink("https://nylzo.example", link.ID, link.KDF, &s) != v.Input {
				t.Fatal("format round trip mismatch")
			}
		})
	}
}

func TestMetaVectors(t *testing.T) {
	f := loadVectors(t)
	for _, v := range f.Meta {
		t.Run(v.Name, func(t *testing.T) {
			m, err := core.DecodeMeta([]byte(v.Input))
			if !v.Valid {
				if err == nil {
					t.Fatalf("expected rejection of %q", v.Input)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			can, err := core.EncodeMeta(m)
			if err != nil {
				t.Fatal(err)
			}
			if string(can) != *v.Canonical {
				t.Fatalf("canonical mismatch:\n got %s\nwant %s", can, *v.Canonical)
			}
			if (m.Text == nil) != (v.Text == nil) || (v.Text != nil && *m.Text != *v.Text) {
				t.Fatal("text mismatch")
			}
			if (m.File == nil) != (v.FileName == nil) {
				t.Fatal("file presence mismatch")
			}
			if m.File != nil &&
				(m.File.Name != *v.FileName || m.File.Mime != *v.FileMime || m.File.Size != *v.FileSize) {
				t.Fatal("file mismatch")
			}
			again, err := core.DecodeMeta(can)
			if err != nil {
				t.Fatal(err)
			}
			can2, _ := core.EncodeMeta(again)
			if string(can2) != string(can) {
				t.Fatal("canonical form is not a fixed point")
			}
		})
	}
}

func TestVectorsPassphraseStrength(t *testing.T) {
	f := loadVectors(t)
	if len(f.Strength) == 0 {
		t.Fatal("no passphrase strength vectors")
	}
	for _, v := range f.Strength {
		err := core.CheckPassphraseStrength(v.Passphrase)
		if (err == nil) != v.Accepted || (err != nil && !errors.Is(err, core.ErrWeakPassphrase)) {
			t.Fatalf("%s: %v", v.Name, err)
		}
	}
}

func TestVectorsRequestSignatures(t *testing.T) {
	f := loadVectors(t)
	if len(f.Request) == 0 {
		t.Fatal("no request vectors")
	}
	for _, v := range f.Request {
		t.Run(v.Name, func(t *testing.T) {
			seed := to32(t, mustHex(t, v.Seed))
			challenge, err := core.DecodeExact(v.Challenge, core.ChallengeLen)
			if err != nil {
				t.Fatal(err)
			}
			body := mustHex(t, v.Body)
			if hex.EncodeToString(
				core.RequestMessage(v.Purpose, v.Method, v.Path, (*[core.ChallengeLen]byte)(challenge), body),
			) != v.Message {
				t.Fatal("request message mismatch")
			}
			sig := core.SignRequest(seed, v.Purpose, v.Method, v.Path, [core.ChallengeLen]byte(challenge), body)
			if core.Encode(sig.PublicKey[:]) != v.PublicKey || core.Encode(sig.Sig[:]) != v.Signature {
				t.Fatal("signature mismatch")
			}
			var parsed core.Signature
			if v.Purpose == core.PurposeVerifier {
				if v.Authorization != "" || sig.VerifierHeader() != v.Verifier {
					t.Fatal("verifier header mismatch")
				}
				parsed, err = core.ParseVerifierHeader(v.Verifier, [core.ChallengeLen]byte(challenge))
			} else {
				if v.Verifier != "" || sig.Authorization() != v.Authorization {
					t.Fatal("authorization header mismatch")
				}
				parsed, err = core.ParseAuthorization(v.Authorization)
			}
			if err != nil || !parsed.Verify(v.Purpose, v.Method, v.Path, body) {
				t.Fatalf("the vector header does not verify: %v", err)
			}
			if parsed.Verify(v.Purpose, v.Method, v.Path+"x", body) ||
				parsed.Verify(v.Purpose, v.Method, v.Path, append(body, 'x')) {
				t.Fatal("a signature verified for another path or body")
			}
		})
	}
}
