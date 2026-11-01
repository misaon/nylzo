package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"math/bits"

	"github.com/misaon/nylzo/internal/erase"
)

const (
	KeyLen       = 32
	LocatorLen   = sha256.Size
	EraseSaltLen = 32
	nonceLen     = 12
	tagLen       = 16
	WrapLen      = nonceLen + KeyLen + tagLen
	RecordBlock  = 512

	labelLocator = "nylzo/v1/locator"
	infoRecord   = "nylzo/v1/record/"
	infoBlob     = "nylzo/v1/blob"
)

type (
	Key       [KeyLen]byte
	Locator   [LocatorLen]byte
	EraseSalt [EraseSaltLen]byte
	Purpose   string
	label     string
)

const (
	Read   Purpose = "read"
	Manage Purpose = "manage"

	labelRecord label = "record"
	labelBlob   label = "blob"
)

var (
	ErrOpen   = errors.New("seal: the key does not open this value")
	ErrErased = errors.New("seal: the erase block of the blob has been overwritten")
)

func LocatorOf(id []byte) Locator {
	return sha256.Sum256(append([]byte(labelLocator), id...))
}

func NewKey() Key {
	var key Key
	rand.Read(key[:])
	return key
}

// KeyFor derives the key that unwraps a record key from a public key the server never stores.
func KeyFor(publicKey []byte, locator Locator, purpose Purpose) Key {
	derived, err := hkdf.Key(sha256.New, publicKey, locator[:], infoRecord+string(purpose), KeyLen)
	if err != nil {
		panic(err)
	}
	return Key(derived)
}

func Wrap(wrapping, key Key, locator Locator, purpose Purpose) []byte {
	return seal(wrapping, key[:], locator, label(purpose))
}

func Unwrap(wrapping Key, wrapped []byte, locator Locator, purpose Purpose) (Key, error) {
	opened, err := open(wrapping, wrapped, locator, label(purpose))
	if err != nil || len(opened) != KeyLen {
		return Key{}, ErrOpen
	}
	return Key(opened), nil
}

func WrapBlobKey(recordKey, blobKey Key, locator Locator) []byte {
	return seal(recordKey, blobKey[:], locator, labelBlob)
}

func UnwrapBlobKey(recordKey Key, wrapped []byte, locator Locator) (Key, error) {
	opened, err := open(recordKey, wrapped, locator, labelBlob)
	if err != nil || len(opened) != KeyLen {
		return Key{}, ErrOpen
	}
	return Key(opened), nil
}

// Record pads the fields so that every sealed record is a multiple of RecordBlock bytes long.
func Record(key Key, locator Locator, fields []byte) []byte {
	padded := RecordBlock * ((len(fields) + nonceLen + tagLen + RecordBlock - 1) / RecordBlock)
	plaintext := make([]byte, padded-nonceLen-tagLen)
	copy(plaintext, fields)
	return seal(key, plaintext, locator, labelRecord)
}

func OpenRecord(key Key, locator Locator, sealed []byte) ([]byte, error) {
	return open(key, sealed, locator, labelRecord)
}

func NewEraseSalt() EraseSalt {
	var salt EraseSalt
	rand.Read(salt[:])
	return salt
}

// EraseBlock is the first block of a stored blob; the server overwrites it in place before it unlinks the file.
func EraseBlock(salt EraseSalt) []byte {
	block := make([]byte, erase.BlockLen)
	copy(block, salt[:])
	return block
}

// ReadEraseBlock reads the salt from the erase block and reports ErrErased for a block the server has overwritten.
func ReadEraseBlock(stored io.Reader) (EraseSalt, error) {
	block := make([]byte, erase.BlockLen)
	defer clear(block)
	if _, err := io.ReadFull(stored, block); err != nil {
		return EraseSalt{}, err
	}
	salt := EraseSalt(block[:EraseSaltLen])
	if salt == (EraseSalt{}) {
		return salt, ErrErased
	}
	return salt, nil
}

func StoredBlobSize(objectSize int64) int64 {
	return erase.BlockLen + PaddedSize(objectSize)
}

// BlobStream encrypts and decrypts a stored blob under a key that needs the salt of its erase block.
func BlobStream(blobKey Key, salt EraseSalt) cipher.Stream {
	derived, err := hkdf.Key(sha256.New, blobKey[:], salt[:], infoBlob, KeyLen)
	if err != nil {
		panic(err)
	}
	//nolint:gosec // G407: every blob has its own random key and salt, so its counter may start at zero.
	return cipher.NewCTR(newBlock(Key(derived)), make([]byte, aes.BlockSize))
}

// PaddedSize is the Padmé length (Nikitin et al., PETS 2019, algorithm 1).
func PaddedSize(length int64) int64 {
	if length < 2 {
		return length
	}
	exponent := bits.Len64(uint64(length)) - 1
	zeros := exponent - bits.Len(uint(exponent))
	if zeros <= 0 {
		return length
	}
	mask := int64(1)<<zeros - 1
	return (length + mask) &^ mask
}

func seal(key Key, plaintext []byte, locator Locator, purpose label) []byte {
	//nolint:gosec // G407: NewGCMWithRandomNonce draws a fresh nonce for every seal.
	return newAEAD(key).Seal(nil, nil, plaintext, additionalData(locator, purpose))
}

func open(key Key, sealed []byte, locator Locator, purpose label) ([]byte, error) {
	opened, err := newAEAD(key).Open(nil, nil, sealed, additionalData(locator, purpose))
	if err != nil {
		return nil, ErrOpen
	}
	return opened, nil
}

func additionalData(locator Locator, purpose label) []byte {
	return append(locator[:len(locator):len(locator)], purpose...)
}

func newAEAD(key Key) cipher.AEAD {
	aead, err := cipher.NewGCMWithRandomNonce(newBlock(key))
	if err != nil {
		panic(err)
	}
	return aead
}

func newBlock(key Key) cipher.Block {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	return block
}
