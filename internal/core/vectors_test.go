package core_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/vectors"
)

var committedVectors = sync.OnceValues(func() (*vectors.File, error) {
	data, err := os.ReadFile("../../spec/vectors.json")
	if err != nil {
		return nil, err
	}
	var file vectors.File
	if err = json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	return &file, nil
})

func loadVectors(t *testing.T) *vectors.File {
	t.Helper()
	file, err := committedVectors()
	if err != nil {
		t.Fatalf("spec/vectors.json: %v", err)
	}
	return file
}

func decodeHex(t *testing.T, text string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func decode32(t *testing.T, text string) *[32]byte {
	t.Helper()
	decoded := decodeHex(t, text)
	if len(decoded) != 32 {
		t.Fatalf("want 32 bytes, got %d", len(decoded))
	}
	return (*[32]byte)(decoded)
}

var rfc7914 = map[string]string{
	"rfc7914-1": "77d6576238657b203b19ca42c18a0497f16b4844e3074ae8dfdffa3fede21442fcd0069ded0948f8326a753a0fc81f17e8d3e0fb2e0d3628cf35e20c38d18906",
	"rfc7914-2": "fdbabe1c9d3472007856e7190d01e9fe7c6ad7cbc8237830e77376634b3731622eaf30d92e22a3886ff109279d9830dac727afb94a83ee6d8360cbdfa2cc0640",
	"rfc7914-3": "7023bdcb3afd7348461c06cd81fd38ebfda8fbba904f8e3ea9b543f6545da1f2d5432955613f0fcf62d49705242a9af9e61e85dc0d651e40dfcf017b45575887",
	"rfc7914-4": "2101cb9b6a511aaeaddbbe09cf70f881ec568d574a2ffd4dabe5ee9820adaa478e56fd8f4ba5d09ffa1c6d927c40f4c337304049e8a952fbcbf45c6fa77a41a4",
}

func TestScryptVectorsAreTheRFC7914Values(t *testing.T) {
	t.Parallel()
	file := loadVectors(t)
	if len(file.Scrypt) != len(rfc7914) {
		t.Fatalf("want %d RFC 7914 vectors, got %d", len(rfc7914), len(file.Scrypt))
	}
	for _, vector := range file.Scrypt {
		if want, ok := rfc7914[vector.Name]; !ok || vector.DK != want {
			t.Fatalf("%s: vectors.json holds %s, RFC 7914 says %s", vector.Name, vector.DK, want)
		}
	}
}

func TestDeriveVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).Derive {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			id, err := core.ParseID(vector.ID)
			if err != nil {
				t.Fatal(err)
			}
			secret := decode32(t, vector.S)
			hsalt := core.HSalt(id)
			link, err := core.DeriveLink(id, secret)
			if err != nil || hex.EncodeToString(hsalt[:]) != vector.HSalt ||
				hex.EncodeToString(link.R[:]) != vector.R || hex.EncodeToString(link.SaltPW[:]) != vector.SaltPW {
				t.Fatalf("link keys: %v", err)
			}
			passphrase := ""
			if vector.Passphrase != nil {
				passphrase = *vector.Passphrase
			}
			passphraseKey, err := core.DerivePassphraseKey(core.KDF(vector.KDFID), passphrase, &link.SaltPW)
			if err != nil || hex.EncodeToString(passphraseKey) != vector.P {
				t.Fatalf("passphrase key: %v", err)
			}
			content, err := core.DeriveContent(id, secret, passphraseKey)
			if err != nil || hex.EncodeToString(content.K[:]) != vector.K ||
				hex.EncodeToString(content.V[:]) != vector.V {
				t.Fatalf("content keys: %v", err)
			}
			readKey, manageKey := core.PublicKey(&link.R), core.PublicKey(decode32(t, vector.M))
			if core.Encode(readKey[:]) != vector.ReadKey || core.Encode(manageKey[:]) != vector.ManageKey {
				t.Fatal("public keys")
			}
			verifierKey := core.PublicKey(&content.V)
			if (vector.KDFID != 0) != (vector.VerifierKey != nil) ||
				(vector.VerifierKey != nil && core.Encode(verifierKey[:]) != *vector.VerifierKey) {
				t.Fatal("verifier key")
			}
			if core.Fragment(core.KDF(vector.KDFID), secret) != vector.Fragment {
				t.Fatal("fragment")
			}
		})
	}
}

func TestNFCPassphrasesAgree(t *testing.T) {
	t.Parallel()
	byName := map[string]vectors.Derive{}
	for _, vector := range loadVectors(t).Derive {
		byName[vector.Name] = vector
	}
	composed, decomposed := byName["scrypt-nfc-composed"], byName["scrypt-nfc-decomposed"]
	if composed.Passphrase == nil || decomposed.Passphrase == nil || *composed.Passphrase == *decomposed.Passphrase {
		t.Fatal("the NFC vectors must use different Unicode forms of one passphrase")
	}
	if composed.ID != decomposed.ID || composed.S != decomposed.S || composed.P != decomposed.P ||
		composed.K != decomposed.K {
		t.Fatal("both Unicode forms must derive identical keys")
	}
}

func TestEnvelopeVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).Envelope {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			checkEnvelopeVector(t, &vector)
		})
	}
}

func checkEnvelopeVector(t *testing.T, vector *vectors.Envelope) {
	t.Helper()
	id, err := core.ParseID(vector.ID)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := ""
	if vector.Passphrase != nil {
		passphrase = *vector.Passphrase
	}
	kdf := core.KDF(vector.KDFID)
	_, content, err := core.DeriveAll(id, decode32(t, vector.S), kdf, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	header := core.Header{Version: core.Version, KDF: kdf, IV: [core.IVLen]byte(decodeHex(t, vector.IV))}
	policy := core.Policy{OpensMax: uint16(vector.OpensMax), TTLSeconds: uint32(vector.TTLSeconds)}
	payload := core.Payload{Meta: core.Meta{Text: vector.Text}, File: fileBytes(t, vector.File)}
	if vector.File != nil {
		payload.Meta.File = &core.FileMeta{
			Name: vector.File.Name,
			Mime: vector.File.Mime,
			Size: int64(vector.File.Size),
		}
	}
	meta, err := core.EncodeMeta(payload.Meta)
	if err != nil || string(meta) != vector.MetaJSON || hex.EncodeToString(core.AAD(header, id, policy)) != vector.AAD {
		t.Fatalf("meta %s or aad differ: %v", meta, err)
	}
	object, err := core.Seal(&content.K, header, id, policy, payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(object)
	inline := vector.Object == nil || core.Encode(object) == *vector.Object
	if len(object) != vector.ObjectLen || len(object)-core.HeaderLen-core.TagLen != vector.PlaintextLen ||
		hex.EncodeToString(sum[:]) != vector.ObjectSHA256 || !inline {
		t.Fatalf("object of %d bytes differs", len(object))
	}
	openedHeader, opened, err := core.Open(&content.K, id, policy, kdf, object)
	if err != nil || openedHeader != header {
		t.Fatalf("open: %v", err)
	}
	if (opened.Meta.Text == nil) != (vector.Text == nil) || (vector.Text != nil && *opened.Meta.Text != *vector.Text) ||
		(opened.Meta.File == nil) != (vector.File == nil) || !bytes.Equal(opened.File, payload.File) {
		t.Fatal("the opened payload differs")
	}
	if vector.File != nil && *opened.Meta.File != *payload.Meta.File {
		t.Fatalf("the opened file meta differs: %+v", opened.Meta.File)
	}
}

func fileBytes(t *testing.T, file *vectors.FileContent) []byte {
	t.Helper()
	if file == nil {
		return nil
	}
	if file.Content == "counter" {
		content := make([]byte, file.Size)
		for i := range content {
			content[i] = byte(i)
		}
		return content
	}
	content := decodeHex(t, file.Content)
	if len(content) != file.Size {
		t.Fatalf("file content length %d differs from size %d", len(content), file.Size)
	}
	return content
}

func TestLinkVectors(t *testing.T) {
	t.Parallel()
	file := loadVectors(t)
	canonical := 0
	for i := range file.Link {
		if file.Link[i].Canonical {
			canonical++
		}
	}
	if canonical == 0 {
		t.Fatal("no canonical link vectors: the format round trip would never run")
	}
	for _, vector := range file.Link {
		link, err := core.ParseLink(vector.Input)
		if !vector.Valid {
			if !errors.Is(err, core.ErrLink) || errors.Is(err, core.ErrLinkVersion) != vector.Newer ||
				errors.Is(err, core.ErrLinkRetiredKDF) != vector.RetiredKDF {
				t.Errorf("%s: got %v, want a link error with newer %v and retired %v",
					vector.Name, err, vector.Newer, vector.RetiredKDF)
			}
			continue
		}
		if err != nil || link.ID.String() != *vector.ID || int(link.KDF) != *vector.KDFID ||
			hex.EncodeToString(link.Secret[:]) != *vector.S {
			t.Errorf("%s: parsed %+v, %v", vector.Name, link, err)
			continue
		}
		if vector.Canonical &&
			core.FormatLink("https://nylzo.example", link.ID, link.KDF, &link.Secret) != vector.Input {
			t.Errorf("%s: the format round trip differs", vector.Name)
		}
	}
}

func TestMetaVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).Meta {
		meta, err := core.DecodeMeta([]byte(vector.Input))
		if !vector.Valid {
			if err == nil {
				t.Errorf("%s: accepted %q", vector.Name, vector.Input)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", vector.Name, err)
			continue
		}
		canonical, err := core.EncodeMeta(meta)
		if err != nil || string(canonical) != *vector.Canonical {
			t.Errorf("%s: canonical %s, want %s (%v)", vector.Name, canonical, *vector.Canonical, err)
		}
		if again, decodeErr := core.DecodeMeta(canonical); decodeErr != nil || again.Validate() != nil {
			t.Errorf("%s: the canonical form does not decode: %v", vector.Name, decodeErr)
		}
		if (meta.Text == nil) != (vector.Text == nil) || (vector.Text != nil && *meta.Text != *vector.Text) ||
			(meta.File == nil) != (vector.FileName == nil) {
			t.Errorf("%s: decoded %+v", vector.Name, meta)
		}
		if meta.File != nil &&
			(meta.File.Name != *vector.FileName || meta.File.Mime != *vector.FileMime || meta.File.Size != *vector.FileSize) {
			t.Errorf("%s: decoded file %+v", vector.Name, meta.File)
		}
	}
}

func TestPassphraseStrengthVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).Strength {
		err := core.CheckPassphraseStrength(vector.Passphrase)
		if (err == nil) != vector.Accepted || (err != nil && !errors.Is(err, core.ErrWeakPassphrase)) {
			t.Errorf("%s: %v", vector.Name, err)
		}
	}
}

func TestRequestVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).Request {
		challenge, err := core.DecodeExact(vector.Challenge, core.ChallengeLen)
		if err != nil {
			t.Fatal(err)
		}
		body := decodeHex(t, vector.Body)
		message := core.RequestMessage(
			vector.Purpose,
			vector.Method,
			vector.Path,
			(*[core.ChallengeLen]byte)(challenge),
			body,
		)
		signature := core.SignRequest(decode32(t, vector.Seed), vector.Purpose, vector.Method, vector.Path,
			[core.ChallengeLen]byte(challenge), body)
		if hex.EncodeToString(message) != vector.Message || core.Encode(signature.PublicKey[:]) != vector.PublicKey ||
			core.Encode(signature.Sig[:]) != vector.Signature {
			t.Fatalf("%s: message or signature differs", vector.Name)
		}
		var parsed core.Signature
		if vector.Purpose == core.PurposeVerifier {
			if vector.Authorization != "" || signature.VerifierHeader() != vector.Verifier {
				t.Fatalf("%s: verifier header differs", vector.Name)
			}
			parsed, err = core.ParseVerifierHeader(vector.Verifier, [core.ChallengeLen]byte(challenge))
		} else {
			if vector.Verifier != "" || signature.Authorization() != vector.Authorization {
				t.Fatalf("%s: authorization header differs", vector.Name)
			}
			parsed, err = core.ParseAuthorization(vector.Authorization)
		}
		if err != nil || parsed != signature || !parsed.Verify(vector.Purpose, vector.Method, vector.Path, body) {
			t.Fatalf("%s: the header does not parse back and verify: %v", vector.Name, err)
		}
		if parsed.Verify(vector.Purpose, vector.Method, vector.Path+"x", body) ||
			parsed.Verify(vector.Purpose, vector.Method, vector.Path, append(body, 'x')) {
			t.Fatalf("%s: a signature verified for another path or body", vector.Name)
		}
	}
}

func TestPoWVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).PoW {
		challenge, err := core.DecodeExact(vector.Challenge, core.PoWChallengeLen)
		if err != nil {
			t.Fatal(err)
		}
		id, err := core.ParseID(vector.ID)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := core.PoWDigest((*[core.PoWChallengeLen]byte)(challenge), id, vector.Counter)
		zeroBits := core.LeadingZeroBits(digest[:])
		if err != nil || hex.EncodeToString(digest[:]) != vector.Digest || zeroBits != vector.ZeroBits ||
			core.PoWToken(vector.Challenge, vector.Counter) != vector.Token {
			t.Fatalf("%s: digest, zero bits or token differ: %v", vector.Name, err)
		}
	}
	challenge := core.Encode(make([]byte, core.PoWChallengeLen))
	for _, difficulty := range []int{-1, core.MaxPoWDifficulty + 1} {
		_, _, err := core.SolvePoW(t.Context(), challenge, core.NewID(), difficulty)
		if !errors.Is(err, core.ErrPoWDifficulty) {
			t.Fatalf("difficulty %d: %v", difficulty, err)
		}
	}
	if _, _, err := core.SolvePoW(t.Context(), challenge[1:], core.NewID(), 0); err == nil {
		t.Fatal("a malformed challenge was accepted")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := core.SolvePoW(cancelled, challenge, core.NewID(), core.MaxPoWDifficulty)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled solve went on: %v", err)
	}
}

func TestRedactionVectors(t *testing.T) {
	t.Parallel()
	file := loadVectors(t)
	if len(file.Redact) == 0 {
		t.Fatal("no redaction vectors")
	}
	for _, vector := range file.Redact {
		if redacted := core.RedactReason(vector.Reason); redacted != vector.Redacted {
			t.Errorf("%s: %q, want %q", vector.Name, redacted, vector.Redacted)
		}
	}
	for reason, long := range map[string]bool{
		"":                                false,
		strings.Repeat("ab ", 400):        true,
		strings.Repeat("a", 3000):         false,
		strings.Repeat("# ", 150):         true,
		strings.Repeat("<", 1000):         false,
		strings.Repeat("ab ", 333) + "ab": true,
	} {
		if core.ReasonTooLong(reason) != long {
			t.Errorf("ReasonTooLong(%.20q) must be %v after redaction, like the web client", reason, long)
		}
	}
}
