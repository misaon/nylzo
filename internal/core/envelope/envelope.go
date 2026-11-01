package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/keys"
)

const (
	Version = 2

	IVLen         = 12
	TagLen        = 16
	HeaderLen     = 15
	CommitmentLen = 32
	PolicyLen     = 6
	MetaLenPrefix = 4
	ObjectSlack   = 64
	PrefixLen     = HeaderLen + CommitmentLen

	MaxTextBytes   = 100 * 1024
	MaxFileBytes   = 5 * 1024 * 1024
	MaxMetaBytes   = 102 * 1024
	MaxObjectBytes = MaxFileBytes + MaxTextBytes + Pad + ObjectSlack

	Pad = 65536

	MinTTLSeconds = 60

	labelCommitment = "nylzo/v2/commitment"
	labelObjectHash = "nylzo/v2/object"
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

// Expected is what a reader knows before it opens an object: the link, and the policy the server reported.
type Expected struct {
	ID         core.ID
	Policy     Policy
	KDF        core.KDF
	ObjectHash *[core.ObjectHashLen]byte
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

// AAD binds the header, the commitment, the id and the policy.
func AAD(header Header, commitment []byte, id core.ID, policy Policy) []byte {
	aad := make([]byte, 0, HeaderLen+len(commitment)+core.IDLen+PolicyLen)
	aad = append(aad, header.Bytes()...)
	aad = append(aad, commitment...)
	aad = append(aad, id[:]...)
	return append(aad, policy.Bytes()...)
}

// Commitment binds the commitment key, and through it the content key, to the header, the id and the policy.
func Commitment(key *[core.KeyLen]byte, header Header, id core.ID, policy Policy) [CommitmentLen]byte {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(labelCommitment))
	mac.Write(header.Bytes())
	mac.Write(id[:])
	mac.Write(policy.Bytes())
	return [CommitmentLen]byte(mac.Sum(nil))
}

// ObjectHash is the H of a link, which pins the object the sender made.
func ObjectHash(object []byte) [core.ObjectHashLen]byte {
	digest := sha256.New()
	digest.Write([]byte(labelObjectHash))
	digest.Write(object)
	return [core.ObjectHashLen]byte(digest.Sum(nil))
}

func ObjectLimit(maxFileBytes int64) int64 {
	return min(
		MaxObjectBytes,
		int64(padded(PrefixLen+MetaLenPrefix+MaxMetaBytes+int(maxFileBytes)+TagLen)),
	)
}

// padded is the length of an object that holds length bytes: whole blocks of Pad (spec section 3).
func padded(length int) int {
	if length%Pad == 0 {
		return length
	}
	return length + Pad - length%Pad
}

func Seal(content *keys.ContentKeys, header Header, id core.ID, policy Policy, payload Payload) ([]byte, error) {
	return sealEnvelope(content, header, id, policy, payload, false)
}

// SealWithCompression compresses the attachment, which the sender has asked for, when that shrinks its size class.
func SealWithCompression(
	content *keys.ContentKeys,
	header Header,
	id core.ID,
	policy Policy,
	payload Payload,
) ([]byte, error) {
	return sealEnvelope(content, header, id, policy, payload, true)
}

func sealEnvelope(
	content *keys.ContentKeys,
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
	defer clear(plaintext)
	aead, err := newAEAD(&content.K)
	if err != nil {
		return nil, err
	}
	commitment := Commitment(&content.Commitment, header, id, policy)
	object := make([]byte, 0, PrefixLen+len(plaintext)+TagLen)
	object = append(append(object, header.Bytes()...), commitment[:]...)
	return aead.Seal(object, header.IV[:], plaintext, AAD(header, commitment[:], id, policy)), nil
}

// Open checks the object hash, the header, the length and the commitment before it decrypts (spec section 3).
func Open(content *keys.ContentKeys, expected *Expected, object []byte) (Header, Payload, error) {
	if !hashMatches(expected.ObjectHash, object) {
		return Header{}, Payload{}, ErrIntegrity
	}
	header, err := ParseHeader(object)
	if err != nil {
		return header, Payload{}, err
	}
	if header.KDF != expected.KDF {
		return header, Payload{}, core.ErrKDFMismatch
	}
	if len(object) > MaxObjectBytes || len(object) < PrefixLen+TagLen || len(object)%Pad != 0 {
		return header, Payload{}, ErrIntegrity
	}
	commitment := object[HeaderLen:PrefixLen]
	want := Commitment(&content.Commitment, header, expected.ID, expected.Policy)
	if !hmac.Equal(commitment, want[:]) {
		return header, Payload{}, ErrIntegrity
	}
	aead, err := newAEAD(&content.K)
	if err != nil {
		return header, Payload{}, err
	}
	plaintext, err := aead.Open(
		nil,
		header.IV[:],
		object[PrefixLen:],
		AAD(header, commitment, expected.ID, expected.Policy),
	)
	if err != nil {
		return header, Payload{}, ErrIntegrity
	}
	defer clear(plaintext)
	payload, err := parsePlaintext(plaintext)
	return header, payload, err
}

func hashMatches(expected *[core.ObjectHashLen]byte, object []byte) bool {
	if expected == nil {
		return false
	}
	actual := ObjectHash(object)
	return subtle.ConstantTimeCompare(actual[:], expected[:]) == 1
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
	overhead := PrefixLen + MetaLenPrefix + TagLen
	file, objectLen := payload.File, padded(overhead+len(meta)+len(payload.File))
	if compress && hasFile {
		encoded := compressAttachment(payload.Meta, meta, file)
		if encoded.objectLen != 0 {
			defer clear(encoded.file)
			defer clear(encoded.meta)
			meta, file, objectLen = encoded.meta, encoded.file, encoded.objectLen
		}
	}
	metaLength := uint(len(meta))
	if metaLength > MaxMetaBytes {
		return nil, ErrMeta
	}
	if objectLen > MaxObjectBytes {
		return nil, ErrTooLarge
	}
	plaintext := make([]byte, objectLen-PrefixLen-TagLen)
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
