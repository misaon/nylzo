package core

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
)

var (
	ErrHeader      = errors.New("core: invalid envelope header")
	ErrFlags       = errors.New("core: unsupported envelope flags")
	ErrVersion     = errors.New("core: unsupported envelope version")
	ErrKDFMismatch = errors.New("core: kdf profile does not match the link fragment")
	ErrIntegrity   = errors.New("core: envelope integrity check failed")
	ErrPlaintext   = errors.New("core: malformed plaintext")
	ErrTooLarge    = errors.New("core: content exceeds limits")
	ErrPolicy      = errors.New("core: invalid policy")
)

type Header struct {
	Version uint8
	KDF     KDF
	Flags   uint8
	IV      [IVLen]byte
}

type Policy struct {
	OpensMax   uint16
	TTLSeconds uint32
}

type Payload struct {
	Meta Meta
	File []byte
}

func NewHeader(kdf KDF) Header {
	return Header{Version: Version, KDF: kdf, IV: NewIV()}
}

func (h Header) Bytes() []byte {
	b := make([]byte, HeaderLen)
	b[0] = h.Version
	b[1] = uint8(h.KDF)
	b[2] = h.Flags
	copy(b[3:], h.IV[:])
	return b
}

func (h Header) Validate() error {
	if h.Version != Version {
		return ErrVersion
	}
	if h.Flags != 0 {
		return ErrFlags
	}
	if !h.KDF.Valid() {
		return ErrKDF
	}
	return nil
}

func ParseHeader(b []byte) (Header, error) {
	var h Header
	if len(b) < HeaderLen {
		return h, ErrHeader
	}
	h.Version = b[0]
	h.KDF = KDF(b[1])
	h.Flags = b[2]
	copy(h.IV[:], b[3:HeaderLen])
	return h, h.Validate()
}

func (p Policy) Validate() error {
	if p.OpensMax == 0 || p.TTLSeconds < MinTTLSeconds {
		return ErrPolicy
	}
	return nil
}

func (p Policy) Bytes() []byte {
	b := make([]byte, PolicyLen)
	binary.BigEndian.PutUint16(b[0:2], p.OpensMax)
	binary.BigEndian.PutUint32(b[2:6], p.TTLSeconds)
	return b
}

func AAD(h Header, id ID, p Policy) []byte {
	out := make([]byte, 0, AADLen)
	out = append(out, h.Bytes()...)
	out = append(out, id[:]...)
	out = append(out, p.Bytes()...)
	return out
}

func PaddedLen(n int, hasFile bool) int {
	unit := PadText
	if hasFile {
		unit = PadFile
	}
	if n%unit == 0 {
		return n
	}
	return n + unit - n%unit
}

func Seal(k *[KeyLen]byte, h Header, id ID, p Policy, payload Payload) ([]byte, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	meta, err := EncodeMeta(payload.Meta)
	if err != nil {
		return nil, err
	}
	hasFile := payload.Meta.File != nil
	if hasFile && int64(len(payload.File)) != payload.Meta.File.Size {
		return nil, ErrMeta
	}
	if !hasFile && len(payload.File) != 0 {
		return nil, ErrMeta
	}
	raw := MetaLenPrefix + len(meta) + len(payload.File)
	total := PaddedLen(raw, hasFile)
	if HeaderLen+total+TagLen > MaxObjectBytes {
		return nil, ErrTooLarge
	}
	plaintext := make([]byte, total)
	binary.BigEndian.PutUint32(plaintext[0:MetaLenPrefix], uint32(len(meta)))
	copy(plaintext[MetaLenPrefix:], meta)
	copy(plaintext[MetaLenPrefix+len(meta):], payload.File)
	aead, err := newAEAD(k)
	if err != nil {
		return nil, err
	}
	object := make([]byte, 0, HeaderLen+total+TagLen)
	object = append(object, h.Bytes()...)
	object = aead.Seal(object, h.IV[:], plaintext, AAD(h, id, p))
	return object, nil
}

func Open(k *[KeyLen]byte, id ID, p Policy, kdf KDF, object []byte) (Header, Payload, error) {
	h, err := ParseHeader(object)
	if err != nil {
		return h, Payload{}, err
	}
	if h.KDF != kdf {
		return h, Payload{}, ErrKDFMismatch
	}
	if len(object) > MaxObjectBytes || len(object) < HeaderLen+TagLen {
		return h, Payload{}, ErrIntegrity
	}
	aead, err := newAEAD(k)
	if err != nil {
		return h, Payload{}, err
	}
	plaintext, err := aead.Open(nil, h.IV[:], object[HeaderLen:], AAD(h, id, p))
	if err != nil {
		return h, Payload{}, ErrIntegrity
	}
	payload, err := parsePlaintext(plaintext)
	return h, payload, err
}

func parsePlaintext(pt []byte) (Payload, error) {
	if len(pt) < MetaLenPrefix {
		return Payload{}, ErrPlaintext
	}
	lenMeta := binary.BigEndian.Uint32(pt[0:MetaLenPrefix])
	if lenMeta > MaxMetaBytes || int64(lenMeta) > int64(len(pt)-MetaLenPrefix) {
		return Payload{}, ErrPlaintext
	}
	meta, err := DecodeMeta(pt[MetaLenPrefix : MetaLenPrefix+lenMeta])
	if err != nil {
		return Payload{}, err
	}
	payload := Payload{Meta: meta}
	if meta.File != nil {
		start := MetaLenPrefix + int64(lenMeta)
		if meta.File.Size > int64(len(pt))-start {
			return Payload{}, ErrPlaintext
		}
		payload.File = append([]byte(nil), pt[start:start+meta.File.Size]...)
	}
	return payload, nil
}

func newAEAD(k *[KeyLen]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
