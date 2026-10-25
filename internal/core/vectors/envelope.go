package vectors

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

const octetStream = "application/octet-stream"

type Envelope struct {
	Name         string       `json:"name"`
	ID           string       `json:"id"`
	S            string       `json:"s"`
	KDFID        int          `json:"kdf_id"`
	Passphrase   *string      `json:"passphrase,omitempty"`
	Flags        int          `json:"flags"`
	IV           string       `json:"iv_hex"`
	OpensMax     int          `json:"opens_max"`
	TTLSeconds   int          `json:"ttl_s"`
	Text         *string      `json:"text,omitempty"`
	File         *FileContent `json:"file,omitempty"`
	MetaJSON     string       `json:"meta_json"`
	PlaintextLen int          `json:"plaintext_len"`
	AAD          string       `json:"aad_hex"`
	ObjectLen    int          `json:"object_len"`
	ObjectSHA256 string       `json:"object_sha256"`
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

var envelopeCases = []envelopeCase{
	{name: "text-ascii", opens: 1, ttl: 86400, text: new("hello nylzo")},
	{
		name:  "text-escapes",
		opens: 2,
		ttl:   3600,
		text:  new("český text \"quoted\" back\\slash\nnewline\ttab \u0001ctrl /slash \u2028 line sep \U0001F600"),
	},
	{name: "text-exact-4096", opens: 1, ttl: 86400, text: new(strings.Repeat("a", 4075))},
	{name: "text-4097", opens: 1, ttl: 86400, text: new(strings.Repeat("a", 4076))},
	{name: "file-empty", opens: 1, ttl: 86400, file: &fileCase{name: "empty.bin", mime: octetStream}},
	{
		name:  "file-one-byte",
		opens: 1,
		ttl:   86400,
		file:  &fileCase{name: "b.bin", mime: octetStream, size: 1, inHex: true},
	},
	{name: "file-65536", opens: 3, ttl: 259200, file: &fileCase{name: "block.bin", size: 65536}},
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
		file:  &fileCase{name: "big.bin", mime: octetStream, size: core.MaxFileBytes},
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
		passphrase: new("he\u0301slo s c\u030ces\u030ctinou"),
		opens:      1,
		ttl:        86400,
		text:       new("nfc"),
	},
	{
		name:       "scrypt-empty-passphrase",
		kdf:        core.KDFScrypt,
		passphrase: new(""),
		opens:      1,
		ttl:        86400,
		text:       new("empty passphrase"),
	},
	{name: "opens-10-ttl-7d", opens: 10, ttl: 604800, text: new("many opens")},
}

func (f *File) envelope() error {
	for _, vector := range envelopeCases {
		envelope, err := envelopeVector(vector)
		if err != nil {
			return err
		}
		f.Envelope = append(f.Envelope, envelope)
	}
	return nil
}

func envelopeVector(vector envelopeCase) (Envelope, error) {
	id := deterministicID("env/" + vector.name)
	secret := deterministic32("env/s/" + vector.name)
	_, content, err := core.DeriveAll(id, secret, vector.kdf, valueOr(vector.passphrase))
	if err != nil {
		return Envelope{}, err
	}
	header := core.Header{
		Version: core.Version,
		KDF:     vector.kdf,
		IV:      [core.IVLen]byte(deterministic("iv/"+vector.name, core.IVLen)),
	}
	policy := core.Policy{OpensMax: vector.opens, TTLSeconds: vector.ttl}
	payload, file := vector.payload()
	meta, err := core.EncodeMeta(payload.Meta)
	if err != nil {
		return Envelope{}, err
	}
	object, err := core.Seal(&content.K, header, id, policy, payload)
	if err != nil {
		return Envelope{}, err
	}
	sum := sha256.Sum256(object)
	envelope := Envelope{
		Name:         vector.name,
		ID:           id.String(),
		S:            hex.EncodeToString(secret[:]),
		KDFID:        int(vector.kdf),
		Passphrase:   vector.passphrase,
		IV:           hex.EncodeToString(header.IV[:]),
		OpensMax:     int(vector.opens),
		TTLSeconds:   int(vector.ttl),
		Text:         vector.text,
		File:         file,
		MetaJSON:     string(meta),
		PlaintextLen: len(object) - core.HeaderLen - core.TagLen,
		AAD:          hex.EncodeToString(core.AAD(header, id, policy)),
		ObjectLen:    len(object),
		ObjectSHA256: hex.EncodeToString(sum[:]),
	}
	if len(object) <= inlineMax {
		envelope.Object = new(core.Encode(object))
	}
	return envelope, nil
}

func (c envelopeCase) payload() (core.Payload, *FileContent) {
	payload := core.Payload{Meta: core.Meta{Text: c.text}}
	if c.file == nil {
		return payload, nil
	}
	payload.Meta.File = &core.FileMeta{Name: c.file.name, Mime: c.file.mime, Size: int64(c.file.size)}
	payload.File = counterBytes(c.file.size)
	content := &FileContent{Name: c.file.name, Mime: c.file.mime, Size: c.file.size, Content: "counter"}
	if c.file.inHex {
		content.Content = hex.EncodeToString(payload.File)
	}
	return payload, content
}
