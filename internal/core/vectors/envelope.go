package vectors

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
)

const octetStream = "application/octet-stream"

type Envelope struct {
	Name         string       `json:"name"`
	ID           string       `json:"id"`
	S            string       `json:"s"`
	KDFID        int          `json:"kdf_id"`
	Passphrase   *string      `json:"passphrase,omitempty"`
	E            string       `json:"e_hex"`
	Flags        int          `json:"flags"`
	IV           string       `json:"iv_hex"`
	OpensMax     int          `json:"opens_max"`
	TTLSeconds   int          `json:"ttl_s"`
	Text         *string      `json:"text,omitempty"`
	File         *FileContent `json:"file,omitempty"`
	MetaJSON     string       `json:"meta_json"`
	PlaintextLen int          `json:"plaintext_len"`
	Commitment   string       `json:"commitment_hex"`
	AAD          string       `json:"aad_hex"`
	ObjectLen    int          `json:"object_len"`
	ObjectSHA256 string       `json:"object_sha256"`
	ObjectHash   string       `json:"h"`
	Link         string       `json:"link"`
	Object       *string      `json:"object_b64,omitempty"`
}

type FileContent struct {
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Size    int    `json:"size"`
	Content string `json:"content"`
}

type envelopeCase struct {
	name       string
	kdf        core.KDF
	passphrase *string
	opens      uint16
	ttl        uint32
	text       *string
	file       *fileCase
}

type fileCase struct {
	name  string
	mime  string
	size  int
	inHex bool
}

// textFilling is the text that makes an object exactly one 64 KiB block: 84 bytes of envelope and meta overhead.
const textFilling = envelope.Pad - 84

var envelopeCases = []envelopeCase{
	{name: "text-ascii", opens: 1, ttl: 86400, text: new("hello nylzo")},
	{
		name:  "text-escapes",
		opens: 2,
		ttl:   3600,
		text:  new("český text \"quoted\" back\\slash\nnewline\ttab \u0001ctrl /slash   line sep \U0001F600"),
	},
	{name: "text-exact-65536", opens: 1, ttl: 86400, text: new(strings.Repeat("a", textFilling))},
	{name: "text-65537", opens: 1, ttl: 86400, text: new(strings.Repeat("a", textFilling+1))},
	{name: "file-empty", opens: 1, ttl: 86400, file: &fileCase{name: "empty.bin", mime: octetStream}},
	{
		name:  "file-one-byte",
		opens: 1,
		ttl:   86400,
		file:  &fileCase{name: "b.bin", mime: octetStream, size: 1, inHex: true},
	},
	{
		name:       "file-and-text-scrypt",
		kdf:        core.KDFScrypt,
		passphrase: new("password"),
		opens:      1,
		ttl:        86400,
		text:       new("příloha v příloze"),
		file:       &fileCase{name: "příloha č.1.pdf", mime: "application/pdf", size: 1000, inHex: true},
	},
	{
		name:  "file-5mib",
		opens: 1,
		ttl:   604800,
		file:  &fileCase{name: "big.bin", mime: octetStream, size: envelope.MaxFileBytes},
	},
	{
		name:       "scrypt-text",
		kdf:        core.KDFScrypt,
		passphrase: new("correct horse battery staple"),
		opens:      1,
		ttl:        86400,
		text:       new("scrypt protected"),
	},
	{
		name:       "scrypt-nfc-decomposed",
		kdf:        core.KDFScrypt,
		passphrase: new("héslo s češtinou"),
		opens:      1,
		ttl:        86400,
		text:       new("nfc"),
	},
	{name: "opens-10-ttl-7d", opens: 10, ttl: 604800, text: new("many opens")},
}

func (f *File) envelope() error {
	for _, vector := range envelopeCases {
		generated, err := envelopeVector(vector)
		if err != nil {
			return err
		}
		f.Envelope = append(f.Envelope, generated)
	}
	return nil
}

type sealedCase struct {
	id      core.ID
	secret  *[core.SecretLen]byte
	share   *[core.ShareLen]byte
	header  envelope.Header
	policy  envelope.Policy
	payload envelope.Payload
	content keys.ContentKeys
	object  []byte
}

func sealCase(vector envelopeCase) (sealedCase, error) {
	sealed := sealedCase{
		id:     deterministicID("env/" + vector.name),
		secret: deterministic32("env/s/" + vector.name),
		share:  deterministic32("env/e/" + vector.name),
		header: envelope.Header{
			Version: envelope.Version,
			KDF:     vector.kdf,
			IV:      [envelope.IVLen]byte(deterministic("iv/"+vector.name, envelope.IVLen)),
		},
		policy: envelope.Policy{OpensMax: vector.opens, TTLSeconds: vector.ttl},
	}
	derived, err := keys.DeriveAll(
		sealed.id,
		sealed.secret,
		vector.kdf,
		valueOr(vector.passphrase),
		sealed.share,
	)
	if err != nil {
		return sealedCase{}, err
	}
	sealed.content = derived.Content
	sealed.payload, _ = vector.payload()
	sealed.object, err = envelope.Seal(&sealed.content, sealed.header, sealed.id, sealed.policy, sealed.payload)
	return sealed, err
}

func envelopeVector(vector envelopeCase) (Envelope, error) {
	sealed, err := sealCase(vector)
	if err != nil {
		return Envelope{}, err
	}
	_, file := vector.payload()
	meta, err := envelope.EncodeMeta(sealed.payload.Meta)
	if err != nil {
		return Envelope{}, err
	}
	object := sealed.object
	commitment := object[envelope.HeaderLen:envelope.PrefixLen]
	sum := sha256.Sum256(object)
	hash := envelope.ObjectHash(object)
	generated := Envelope{
		Name:         vector.name,
		ID:           sealed.id.String(),
		S:            hex.EncodeToString(sealed.secret[:]),
		KDFID:        int(vector.kdf),
		Passphrase:   vector.passphrase,
		IV:           hex.EncodeToString(sealed.header.IV[:]),
		OpensMax:     int(vector.opens),
		TTLSeconds:   int(vector.ttl),
		Text:         vector.text,
		File:         file,
		MetaJSON:     string(meta),
		E:            hex.EncodeToString(sealed.share[:]),
		PlaintextLen: len(object) - envelope.PrefixLen - envelope.TagLen,
		Commitment:   hex.EncodeToString(commitment),
		AAD:          hex.EncodeToString(envelope.AAD(sealed.header, commitment, sealed.id, sealed.policy)),
		ObjectLen:    len(object),
		ObjectSHA256: hex.EncodeToString(sum[:]),
		ObjectHash:   core.Encode(hash[:]),
		Link:         core.FormatLink(base, sealed.id, vector.kdf, sealed.secret, &hash),
	}
	if len(object) <= inlineMax {
		generated.Object = new(core.Encode(object))
	}
	return generated, nil
}

func (c envelopeCase) payload() (envelope.Payload, *FileContent) {
	payload := envelope.Payload{Meta: envelope.Meta{Text: c.text}}
	if c.file == nil {
		return payload, nil
	}
	payload.Meta.File = &envelope.FileMeta{Name: c.file.name, Mime: c.file.mime, Size: int64(c.file.size)}
	payload.File = counterBytes(c.file.size)
	content := &FileContent{Name: c.file.name, Mime: c.file.mime, Size: c.file.size, Content: "counter"}
	if c.file.inHex {
		content.Content = hex.EncodeToString(payload.File)
	}
	return payload, content
}
