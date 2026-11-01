package core_test

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/core/report"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
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
			if hsalt := keys.HSalt(id); hex.EncodeToString(hsalt[:]) != vector.HSalt {
				t.Fatal("hsalt")
			}
			link, err := keys.DeriveLink(id, secret)
			if err != nil || hex.EncodeToString(link.R[:]) != vector.R ||
				hex.EncodeToString(link.PassphraseSalt[:]) != vector.PassphraseSalt {
				t.Fatalf("link keys: %v", err)
			}
			passphraseKey, err := keys.DerivePassphraseKey(
				core.KDF(vector.KDFID),
				valueOf(vector.Passphrase),
				&link.PassphraseSalt,
			)
			if err != nil || hex.EncodeToString(passphraseKey) != vector.P {
				t.Fatalf("passphrase key: %v", err)
			}
			content, err := keys.DeriveContent(id, secret, passphraseKey, decode32(t, vector.E))
			if err != nil || hex.EncodeToString(content.K[:]) != vector.K ||
				hex.EncodeToString(content.Commitment[:]) != vector.Commitment {
				t.Fatalf("content keys: %v", err)
			}
			verifier, err := keys.DeriveVerifier(id, secret, passphraseKey)
			if err != nil || hex.EncodeToString(verifier[:]) != vector.V {
				t.Fatalf("verifier: %v", err)
			}
			readKey, manageKey := signing.PublicKey(&link.R), signing.PublicKey(decode32(t, vector.M))
			if core.Encode(readKey[:]) != vector.ReadKey || core.Encode(manageKey[:]) != vector.ManageKey {
				t.Fatal("public keys")
			}
			if verifierKey := signing.PublicKey(&verifier); core.Encode(verifierKey[:]) != vector.VerifierKey {
				t.Fatal("verifier key")
			}
		})
	}
}

func valueOf(text *string) string {
	if text == nil {
		return ""
	}
	return *text
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
	kdf := core.KDF(vector.KDFID)
	derived, err := keys.DeriveAll(id, decode32(t, vector.S), kdf, valueOf(vector.Passphrase), decode32(t, vector.E))
	if err != nil {
		t.Fatal(err)
	}
	content := derived.Content
	header := envelope.Header{Version: envelope.Version, KDF: kdf, IV: [envelope.IVLen]byte(decodeHex(t, vector.IV))}
	policy := envelope.Policy{OpensMax: uint16(vector.OpensMax), TTLSeconds: uint32(vector.TTLSeconds)}
	payload := envelope.Payload{Meta: envelope.Meta{Text: vector.Text}, File: fileBytes(t, vector.File)}
	if vector.File != nil {
		payload.Meta.File = &envelope.FileMeta{
			Name: vector.File.Name,
			Mime: vector.File.Mime,
			Size: int64(vector.File.Size),
		}
	}
	meta, err := envelope.EncodeMeta(payload.Meta)
	if err != nil || string(meta) != vector.MetaJSON {
		t.Fatalf("meta %s differs: %v", meta, err)
	}
	object, err := envelope.Seal(&content, header, id, policy, payload)
	if err != nil {
		t.Fatal(err)
	}
	prefix := envelope.PrefixLen
	commitment := object[envelope.HeaderLen:prefix]
	if hex.EncodeToString(envelope.AAD(header, commitment, id, policy)) != vector.AAD ||
		hex.EncodeToString(commitment) != vector.Commitment {
		t.Fatal("aad or commitment differ")
	}
	sum := sha256.Sum256(object)
	inline := vector.Object == nil || core.Encode(object) == *vector.Object
	if len(object) != vector.ObjectLen || len(object)-prefix-envelope.TagLen != vector.PlaintextLen ||
		hex.EncodeToString(sum[:]) != vector.ObjectSHA256 || !inline {
		t.Fatalf("object of %d bytes differs", len(object))
	}
	hash := envelope.ObjectHash(object)
	link, linkErr := core.ParseLink(vector.Link)
	if linkErr != nil || core.Encode(hash[:]) != vector.ObjectHash || link.ObjectHash != hash {
		t.Fatalf("the object hash or the link differ: %v", linkErr)
	}
	expected := envelope.Expected{ID: id, Policy: policy, KDF: kdf, ObjectHash: &hash}
	openedHeader, opened, err := envelope.Open(&content, &expected, object)
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
				errors.Is(err, core.ErrLinkRetiredKDF) != vector.RetiredKDF ||
				errors.Is(err, core.ErrLinkReleased) != vector.Released {
				t.Errorf("%s: got %v, want a link error with newer %v, retired %v and released %v",
					vector.Name, err, vector.Newer, vector.RetiredKDF, vector.Released)
			}
			continue
		}
		if err != nil || link.ID.String() != *vector.ID || int(link.KDF) != *vector.KDFID ||
			hex.EncodeToString(link.Secret[:]) != *vector.S ||
			hex.EncodeToString(link.ObjectHash[:]) != *vector.ObjectHash {
			t.Errorf("%s: parsed %+v, %v", vector.Name, link, err)
			continue
		}
		formatted := core.FormatLink("https://nylzo.example", link.ID, link.KDF, &link.Secret, &link.ObjectHash)
		if vector.Canonical && formatted != vector.Input {
			t.Errorf("%s: the format round trip differs", vector.Name)
		}
	}
}

func TestMetaVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).Meta {
		meta, err := envelope.DecodeMeta([]byte(vector.Input))
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
		canonical, err := envelope.EncodeMeta(meta)
		if err != nil || string(canonical) != *vector.Canonical {
			t.Errorf("%s: canonical %s, want %s (%v)", vector.Name, canonical, *vector.Canonical, err)
		}
		if again, decodeErr := envelope.DecodeMeta(canonical); decodeErr != nil || again.Validate() != nil {
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
	file := loadVectors(t)
	for _, vector := range file.Strength {
		err := keys.CheckPassphraseStrength(vector.Passphrase)
		if (err == nil) != vector.Accepted || (err != nil && !errors.Is(err, keys.ErrWeakPassphrase)) {
			t.Errorf("%s: %v", vector.Name, err)
		}
		if weak, ok := errors.AsType[*keys.WeakPassphraseError](err); ok && weak.Problem != vector.Problem {
			t.Errorf("%s: problem %q, want %q", vector.Name, weak.Problem, vector.Problem)
		}
	}
	assigned := sha256.Sum256([]byte(keys.AssignedCodePointList))
	if hex.EncodeToString(assigned[:]) != file.AssignedHash || file.AssignedVersion != keys.AssignedUnicodeVersion {
		t.Fatal("the assigned code point table differs from the vectors")
	}
}

func TestRejectedEnvelopeVectors(t *testing.T) {
	t.Parallel()
	file := loadVectors(t)
	byName := map[string]vectors.Envelope{}
	for _, vector := range file.Envelope {
		byName[vector.Name] = vector
	}
	for _, vector := range file.Rejected {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			err := openRejected(t, byName, &vector)
			if got := rejectionName(err); got != vector.Error {
				t.Fatalf("got %q (%v), want %q", got, err, vector.Error)
			}
		})
	}
}

func openRejected(t *testing.T, envelopes map[string]vectors.Envelope, vector *vectors.Rejected) error {
	t.Helper()
	base := envelopes[vector.Base]
	id, err := core.ParseID(base.ID)
	if err != nil {
		t.Fatal(err)
	}
	policy := envelope.Policy{OpensMax: uint16(base.OpensMax), TTLSeconds: uint32(base.TTLSeconds)}
	var object []byte
	if vector.Object != nil {
		if object, err = core.DecodeExact(*vector.Object, (len(*vector.Object)*6)/8); err != nil {
			t.Fatal(err)
		}
	} else {
		source := base
		if vector.ObjectFrom != "" {
			source = envelopes[vector.ObjectFrom]
		}
		object = sealVector(t, &source)
	}
	if vector.FlipByte != nil {
		object[*vector.FlipByte] ^= 1
	}
	if vector.SetByte != nil {
		object[vector.SetByte.Offset] = vector.SetByte.Value
	}
	if vector.TruncateTo != nil {
		object = object[:*vector.TruncateTo]
	}
	hash, err := core.Decode32(vector.ObjectHash)
	if err != nil {
		t.Fatal(err)
	}
	if vector.ReaderID != nil {
		if id, err = core.ParseID(*vector.ReaderID); err != nil {
			t.Fatal(err)
		}
	}
	if vector.OpensMax != nil {
		policy.OpensMax = uint16(*vector.OpensMax)
	}
	if vector.TTLSeconds != nil {
		policy.TTLSeconds = uint32(*vector.TTLSeconds)
	}
	kdf := core.KDF(vector.KDFID)
	derived, err := keys.DeriveAll(
		id,
		decode32(t, base.S),
		kdf,
		valueOf(vector.Passphrase),
		decode32(t, vector.E),
	)
	if err != nil {
		t.Fatal(err)
	}
	expected := envelope.Expected{ID: id, Policy: policy, KDF: kdf, ObjectHash: hash}
	_, _, err = envelope.Open(&derived.Content, &expected, object)
	return err
}

func sealVector(t *testing.T, vector *vectors.Envelope) []byte {
	t.Helper()
	id, err := core.ParseID(vector.ID)
	if err != nil {
		t.Fatal(err)
	}
	kdf := core.KDF(vector.KDFID)
	derived, err := keys.DeriveAll(id, decode32(t, vector.S), kdf, valueOf(vector.Passphrase), decode32(t, vector.E))
	if err != nil {
		t.Fatal(err)
	}
	header := envelope.Header{Version: envelope.Version, KDF: kdf, IV: [envelope.IVLen]byte(decodeHex(t, vector.IV))}
	policy := envelope.Policy{OpensMax: uint16(vector.OpensMax), TTLSeconds: uint32(vector.TTLSeconds)}
	object, err := envelope.Seal(
		&derived.Content,
		header,
		id,
		policy,
		envelope.Payload{Meta: envelope.Meta{Text: vector.Text}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func rejectionName(err error) string {
	switch {
	case errors.Is(err, envelope.ErrIntegrity):
		return "integrity"
	case errors.Is(err, envelope.ErrPlaintext), errors.Is(err, envelope.ErrMeta):
		return "plaintext"
	case errors.Is(err, envelope.ErrVersion):
		return "version"
	case errors.Is(err, envelope.ErrFlags):
		return "flags"
	case errors.Is(err, core.ErrKDFMismatch):
		return "kdf_mismatch"
	case errors.Is(err, core.ErrKDF):
		return "kdf"
	case errors.Is(err, envelope.ErrHeader):
		return "header"
	case err == nil:
		return "accepted"
	default:
		return err.Error()
	}
}

func TestShareKeysOfSmallOrderAreRefused(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).ShareKeyRejected {
		if _, err := share.ParseKey(vector.Key); err == nil {
			t.Fatalf("%s: a share key of small order is accepted", vector.Name)
		}
	}
}

func TestShareVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).Share {
		client, err := ecdh.X25519().NewPrivateKey(decodeHex(t, vector.ClientPrivate))
		if err != nil || core.Encode(client.PublicKey().Bytes()) != vector.ClientPublic {
			t.Fatalf("%s: client key: %v", vector.Name, err)
		}
		wrapped, err := core.DecodeExact(vector.Wrapped, share.WrappedLen)
		if err != nil {
			t.Fatal(err)
		}
		unwrapped, err := share.Unwrap(client, (*[share.WrappedLen]byte)(wrapped))
		if !vector.Valid {
			if err == nil {
				t.Fatalf("%s: unwrapped a share it must refuse", vector.Name)
			}
			continue
		}
		if err != nil || hex.EncodeToString(unwrapped[:]) != *vector.E {
			t.Fatalf("%s: unwrap: %v", vector.Name, err)
		}
		server, err := ecdh.X25519().NewPrivateKey(decodeHex(t, *vector.ServerPrivate))
		if err != nil {
			t.Fatal(err)
		}
		again, err := share.WrapWith(server, &unwrapped, client.PublicKey())
		if err != nil || core.Encode(again[:]) != vector.Wrapped {
			t.Fatalf("%s: wrap: %v", vector.Name, err)
		}
	}
}

func TestCompressedLengthVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).CompressedLength {
		if got := envelope.CompressedLength(vector.Original, vector.Needed); got != vector.ObjectLen {
			t.Errorf("%d from %d: got %d, want %d", vector.Needed, vector.Original, got, vector.ObjectLen)
		}
	}
}

func TestRequestVectors(t *testing.T) {
	t.Parallel()
	for _, vector := range loadVectors(t).Request {
		challenge, err := core.DecodeExact(vector.Challenge, signing.ChallengeLen)
		if err != nil {
			t.Fatal(err)
		}
		body := decodeHex(t, vector.Body)
		message := signing.RequestMessage(
			vector.Purpose,
			vector.Method,
			vector.Path,
			(*[signing.ChallengeLen]byte)(challenge),
			body,
		)
		signature := signing.SignRequest(decode32(t, vector.Seed), vector.Purpose, vector.Method, vector.Path,
			[signing.ChallengeLen]byte(challenge), body)
		if hex.EncodeToString(message) != vector.Message || core.Encode(signature.PublicKey[:]) != vector.PublicKey ||
			core.Encode(signature.Value[:]) != vector.Signature {
			t.Fatalf("%s: message or signature differs", vector.Name)
		}
		var parsed signing.Signature
		if vector.Purpose == signing.PurposeVerifier {
			if vector.Authorization != "" || signature.VerifierHeader() != vector.Verifier {
				t.Fatalf("%s: verifier header differs", vector.Name)
			}
			parsed, err = signing.ParseVerifierHeader(vector.Verifier, [signing.ChallengeLen]byte(challenge))
		} else {
			if vector.Verifier != "" || signature.Authorization() != vector.Authorization {
				t.Fatalf("%s: authorization header differs", vector.Name)
			}
			parsed, err = signing.ParseAuthorization(vector.Authorization)
		}
		if err != nil || parsed != *signature || !parsed.Verify(vector.Purpose, vector.Method, vector.Path, body) {
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
		challenge, err := core.DecodeExact(vector.Challenge, proofofwork.ChallengeLen)
		if err != nil {
			t.Fatal(err)
		}
		id, err := core.ParseID(vector.ID)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := proofofwork.Digest((*[proofofwork.ChallengeLen]byte)(challenge), id, vector.Counter)
		zeroBits := proofofwork.LeadingZeroBits(digest[:])
		if err != nil || hex.EncodeToString(digest[:]) != vector.Digest || zeroBits != vector.ZeroBits ||
			proofofwork.Token(vector.Challenge, vector.Counter) != vector.Token {
			t.Fatalf("%s: digest, zero bits or token differ: %v", vector.Name, err)
		}
	}
	challenge := core.Encode(make([]byte, proofofwork.ChallengeLen))
	for _, difficulty := range []int{-1, proofofwork.MaxDifficulty + 1} {
		_, err := proofofwork.Solve(t.Context(), challenge, core.NewID(), difficulty)
		if !errors.Is(err, proofofwork.ErrDifficulty) {
			t.Fatalf("difficulty %d: %v", difficulty, err)
		}
	}
	if _, err := proofofwork.Solve(t.Context(), challenge[1:], core.NewID(), 0); err == nil {
		t.Fatal("a malformed challenge was accepted")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := proofofwork.Solve(cancelled, challenge, core.NewID(), proofofwork.MaxDifficulty)
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
		if redacted := report.RedactReason(vector.Reason); redacted != vector.Redacted {
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
		if report.ReasonTooLong(reason) != long {
			t.Errorf("ReasonTooLong(%.20q) must be %v after redaction, like the web client", reason, long)
		}
	}
}
