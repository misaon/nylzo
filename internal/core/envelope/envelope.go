package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"

	"github.com/misaon/nylzo/internal/core"
)

const (
	Version = 1

	IVLen         = 12
	TagLen        = 16
	HeaderLen     = 15
	PolicyLen     = 6
	AADLen        = HeaderLen + core.IDLen + PolicyLen
	MetaLenPrefix = 4
	ObjectSlack   = 64

	MaxTextBytes   = 100 * 1024
	MaxFileBytes   = 5 * 1024 * 1024
	MaxMetaBytes   = 102 * 1024
	MaxObjectBytes = MaxFileBytes + MaxTextBytes + PadFile + ObjectSlack

	PadText = 4096
	PadFile = 65536

	MinTTLSeconds = 60
)

var (
	ErrHeader    = errors.New("core: invalid envelope header")
	ErrFlags     = errors.New("core: unsupported envelope flags")
	ErrVersion   = errors.New("core: unsupported envelope version")
	ErrIntegrity = errors.New("core: envelope integrity check failed")
	ErrPlaintext = errors.New("core: malformed plaintext")
	ErrTooLarge  = errors.New("core: content exceeds limits")
	ErrPolicy    = errors.New("core: invalid policy")
)

type Header struct {
	Version uint8
	KDF     core.KDF
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

func NewHeader(kdf core.KDF) Header {
	header := Header{Version: Version, KDF: kdf}
	rand.Read(header.IV[:])
	return header
}

func ParseHeader(data []byte) (Header, error) {
	var header Header
	if len(data) < HeaderLen {
		return header, ErrHeader
	}
	header.Version = data[0]
	header.KDF = core.KDF(data[1])
	header.Flags = data[2]
	copy(header.IV[:], data[3:HeaderLen])
	return header, header.Validate()
}

func (h Header) Bytes() []byte {
	return append(append(make([]byte, 0, HeaderLen), h.Version, byte(h.KDF), h.Flags), h.IV[:]...)
}

func (h Header) Validate() error {
	if h.Version != Version {
		return ErrVersion
	}
	if h.Flags != 0 {
		return ErrFlags
	}
	if !h.KDF.Valid() {
		return core.ErrKDF
	}
	return nil
}

func (p Policy) Validate() error {
	if p.OpensMax == 0 || p.TTLSeconds < MinTTLSeconds {
		return ErrPolicy
	}
	return nil
}

func (p Policy) Bytes() []byte {
	return binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint16(nil, p.OpensMax), p.TTLSeconds)
}

func AAD(header Header, id core.ID, policy Policy) []byte {
	aad := make([]byte, 0, AADLen)
	aad = append(aad, header.Bytes()...)
	aad = append(aad, id[:]...)
	return append(aad, policy.Bytes()...)
}

func ObjectLimit(maxFileBytes int64) int64 {
	return min(MaxObjectBytes, int64(PaddedLen(HeaderLen+MetaLenPrefix+MaxMetaBytes+int(maxFileBytes)+TagLen, true)))
}

func PaddedLen(length int, hasFile bool) int {
	unit := PadText
	if hasFile {
		unit = PadFile
	}
	if length%unit == 0 {
		return length
	}
	return length + unit - length%unit
}

func Seal(key *[core.KeyLen]byte, header Header, id core.ID, policy Policy, payload Payload) ([]byte, error) {
	return sealEnvelope(key, header, id, policy, payload, false)
}

// SealWithCompression compresses a large attachment only when it reduces the padded object size.
func SealWithCompression(
	key *[core.KeyLen]byte,
	header Header,
	id core.ID,
	policy Policy,
	payload Payload,
) ([]byte, error) {
	return sealEnvelope(key, header, id, policy, payload, true)
}

func sealEnvelope(
	key *[core.KeyLen]byte,
	header Header,
	id core.ID,
	policy Policy,
	payload Payload,
	compress bool,
) ([]byte, error) {
	if err := header.Validate(); err != nil {
		return nil, err
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	plaintext, err := padPlaintext(payload, compress)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	object := make([]byte, 0, HeaderLen+len(plaintext)+TagLen)
	object = append(object, header.Bytes()...)
	return aead.Seal(object, header.IV[:], plaintext, AAD(header, id, policy)), nil
}

// MayHoldFile tells from an object length whether the envelope can carry a file (spec section 3, D16).
func MayHoldFile(objectBytes int64) bool {
	return objectBytes > 0 && objectBytes%PadFile == 0
}

func Open(key *[core.KeyLen]byte, id core.ID, policy Policy, kdf core.KDF, object []byte) (Header, Payload, error) {
	header, err := ParseHeader(object)
	if err != nil {
		return header, Payload{}, err
	}
	if header.KDF != kdf {
		return header, Payload{}, core.ErrKDFMismatch
	}
	if len(object) > MaxObjectBytes || len(object) < HeaderLen+TagLen {
		return header, Payload{}, ErrIntegrity
	}
	aead, err := newAEAD(key)
	if err != nil {
		return header, Payload{}, err
	}
	plaintext, err := aead.Open(nil, header.IV[:], object[HeaderLen:], AAD(header, id, policy))
	if err != nil {
		return header, Payload{}, ErrIntegrity
	}
	defer clear(plaintext)
	payload, err := parsePlaintext(plaintext)
	return header, payload, err
}

func padPlaintext(payload Payload, compress bool) ([]byte, error) {
	meta, err := EncodeMeta(payload.Meta)
	if err != nil {
		return nil, err
	}
	defer clear(meta)
	hasFile := payload.Meta.File != nil
	if hasFile && int64(len(payload.File)) != payload.Meta.File.Size {
		return nil, ErrMeta
	}
	if !hasFile && len(payload.File) != 0 {
		return nil, ErrMeta
	}
	file := payload.File
	if compress && hasFile {
		encoded := compressAttachment(payload.Meta, meta, file)
		meta, file = encoded.meta, encoded.file
		if len(file) != len(payload.File) {
			defer clear(file)
			defer clear(meta)
		}
	}
	metaLength := uint(len(meta))
	if metaLength > MaxMetaBytes {
		return nil, ErrMeta
	}
	total := PaddedLen(HeaderLen+MetaLenPrefix+len(meta)+len(file)+TagLen, hasFile) - HeaderLen - TagLen
	if HeaderLen+total+TagLen > MaxObjectBytes {
		return nil, ErrTooLarge
	}
	plaintext := make([]byte, total)
	binary.BigEndian.PutUint32(plaintext, uint32(metaLength))
	copy(plaintext[MetaLenPrefix:], meta)
	copy(plaintext[MetaLenPrefix+len(meta):], file)
	return plaintext, nil
}

func parsePlaintext(plaintext []byte) (Payload, error) {
	if len(plaintext) < MetaLenPrefix {
		return Payload{}, ErrPlaintext
	}
	metaLength := int64(binary.BigEndian.Uint32(plaintext))
	contentStart := MetaLenPrefix + metaLength
	if metaLength > MaxMetaBytes || contentStart > int64(len(plaintext)) {
		return Payload{}, ErrPlaintext
	}
	stored, err := decodeStoredMeta(plaintext[MetaLenPrefix:contentStart])
	if err != nil {
		return Payload{}, err
	}
	meta := stored.meta
	if meta.File == nil {
		return Payload{Meta: meta}, nil
	}
	size := meta.File.Size
	if stored.compressedSize > 0 {
		size = stored.compressedSize
	}
	if size > int64(len(plaintext))-contentStart {
		return Payload{}, ErrPlaintext
	}
	file := plaintext[contentStart : contentStart+size]
	if stored.compressedSize > 0 {
		file, err = decompressAttachment(file, meta.File.Size)
		if err != nil {
			return Payload{}, err
		}
	} else {
		file = append([]byte(nil), file...)
	}
	return Payload{Meta: meta, File: file}, nil
}

func newAEAD(key *[core.KeyLen]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
