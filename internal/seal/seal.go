package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"hash"
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
	// SealedWrapLen is the length of a read or manage wrap with its pepper layer.
	SealedWrapLen = nonceLen + WrapLen + tagLen
	BlobTagLen    = sha256.Size
	// BlobWrapLen is the length of a blob wrap that holds the tags of its blob; one of an earlier release is WrapLen.
	BlobWrapLen = nonceLen + KeyLen + 2*BlobTagLen + tagLen
	RecordBlock = 1024
	// RegistrationLen is an OPAQUE registration record (spec section 2.1); SealedRegistrationLen is its wrap.
	RegistrationLen       = 192
	SealedRegistrationLen = nonceLen + RegistrationLen + tagLen

	labelLocator = "nylzo/v1/locator"
	infoRecord   = "nylzo/v1/record/"
	infoBlob     = "nylzo/v1/blob"
	infoBlobTag  = "nylzo/v2/blob/tag"
	infoSaltTag  = "nylzo/v2/blob/salt"
	infoFields   = "nylzo/v2/record/fields"
	infoBlobWrap = "nylzo/v2/record/blob"
	infoPepper   = "nylzo/v2/record/pepper"
	infoRegister = "nylzo/v3/record/registration"
)

type (
	Key       [KeyLen]byte
	Locator   [LocatorLen]byte
	EraseSalt [EraseSaltLen]byte
	BlobTag   [BlobTagLen]byte
	Purpose   string
	label     string
)

const (
	Read   Purpose = "read"
	Manage Purpose = "manage"

	labelRecord       label = "record"
	labelBlob         label = "blob"
	labelPepper       label = "pepper-"
	labelRegistration label = "registration"
)

var (
	ErrOpen        = errors.New("seal: the key does not open this value")
	ErrErased      = errors.New("seal: the erase block of the blob has been overwritten")
	ErrBlobChanged = errors.New("seal: the stored blob differs from the one the server wrote")
)

func LocatorOf(id []byte) Locator {
	return sha256.Sum256(append([]byte(labelLocator), id...))
}

func NewKey() Key {
	var key Key
	rand.Read(key[:])
	return key
}

// WrapRecordKey wraps the record key under a key from the public key and seals that wrap under a key from the pepper,
// so opening it takes both: a public key travels in every signed request, the pepper never enters the database.
func WrapRecordKey(pepperKey Key, publicKey []byte, recordKey Key, locator Locator, purpose Purpose) []byte {
	wrapping := keyFor(publicKey, locator, purpose)
	defer clear(wrapping[:])
	wrapped := seal(wrapping, recordKey[:], locator, label(purpose))
	defer clear(wrapped)
	return sealWrap(pepperKey, wrapped, locator, purpose)
}

// UnwrapRecordKey opens both layers even after the first one failed, so a wrong pepper key costs what a wrong public
// key does.
func UnwrapRecordKey(pepperKey Key, publicKey, sealed []byte, locator Locator, purpose Purpose) (Key, error) {
	wrapped, pepperErr := openSealedWrap(pepperKey, sealed, locator, purpose)
	defer clear(wrapped)
	wrapping := keyFor(publicKey, locator, purpose)
	defer clear(wrapping[:])
	opened, err := open(wrapping, wrapped, locator, label(purpose))
	defer clear(opened)
	if pepperErr != nil || err != nil || len(opened) != KeyLen {
		return Key{}, ErrOpen
	}
	return Key(opened), nil
}

// ResealWrap puts a stored wrap under the current pepper key. A wrap of an earlier release has no pepper layer and is
// WrapLen bytes long; one sealed under an earlier pepper key opens with it. Both are sealed again in place, and a wrap
// that no key opens stays as it is and is reported as ErrOpen.
func ResealWrap(current Key, earlier []Key, wrap *[]byte, locator Locator, purpose Purpose) (bool, error) {
	if len(*wrap) == WrapLen {
		*wrap = sealWrap(current, *wrap, locator, purpose)
		return true, nil
	}
	if inner, err := openSealedWrap(current, *wrap, locator, purpose); err == nil {
		clear(inner)
		return false, nil
	}
	for _, key := range earlier {
		if inner, err := openSealedWrap(key, *wrap, locator, purpose); err == nil {
			*wrap = sealWrap(current, inner, locator, purpose)
			clear(inner)
			return true, nil
		}
	}
	return false, ErrOpen
}

// keyFor derives the key of the inner wrap from a public key the server never stores.
func keyFor(publicKey []byte, locator Locator, purpose Purpose) Key {
	derived, err := hkdf.Key(sha256.New, publicKey, locator[:], infoRecord+string(purpose), KeyLen)
	if err != nil {
		panic(err)
	}
	defer clear(derived)
	return Key(derived)
}

func sealWrap(pepperKey Key, wrapped []byte, locator Locator, purpose Purpose) []byte {
	layer := subkey(pepperKey, locator, infoPepper)
	defer clear(layer[:])
	return seal(layer, wrapped, locator, labelPepper+label(purpose))
}

// openSealedWrap gives zeros of a wrap's length when the pepper layer does not open, so the inner wrap is opened all
// the same.
func openSealedWrap(pepperKey Key, sealed []byte, locator Locator, purpose Purpose) ([]byte, error) {
	layer := subkey(pepperKey, locator, infoPepper)
	defer clear(layer[:])
	wrapped, err := open(layer, sealed, locator, labelPepper+label(purpose))
	if err != nil || len(wrapped) != WrapLen {
		clear(wrapped)
		return make([]byte, WrapLen), ErrOpen
	}
	return wrapped, nil
}

// BlobKey is what a blob wrap holds. A wrap that a release before Nylzo 0.14.0 stored has no tags.
type BlobKey struct {
	Key     Key
	Tag     BlobTag
	SaltTag BlobTag
	Tagged  bool
}

// NewBlobKey is the key of a new blob, whose tags follow once the blob is stored.
func NewBlobKey() BlobKey {
	return BlobKey{Key: NewKey(), Tagged: true}
}

func (b *BlobKey) Clear() {
	clear(b.Key[:])
	clear(b.Tag[:])
	clear(b.SaltTag[:])
}

// WrapBlobKey seals the tags with the key, so no new wrap can skip the check.
func WrapBlobKey(recordKey Key, blob *BlobKey, locator Locator) []byte {
	if !blob.Tagged {
		panic("seal: a blob key is wrapped only with its tags")
	}
	plaintext := make([]byte, 0, KeyLen+2*BlobTagLen)
	defer clear(plaintext[:cap(plaintext)])
	plaintext = append(append(append(plaintext, blob.Key[:]...), blob.Tag[:]...), blob.SaltTag[:]...)
	return seal(subkey(recordKey, locator, infoBlobWrap), plaintext, locator, labelBlob)
}

func UnwrapBlobKey(recordKey Key, wrapped []byte, locator Locator) (BlobKey, error) {
	opened, err := open(subkey(recordKey, locator, infoBlobWrap), wrapped, locator, labelBlob)
	defer clear(opened)
	switch {
	case err != nil:
		return BlobKey{}, ErrOpen
	case len(opened) == KeyLen:
		return BlobKey{Key: Key(opened)}, nil
	case len(opened) == KeyLen+2*BlobTagLen:
		return BlobKey{
			Key:     Key(opened[:KeyLen]),
			Tag:     BlobTag(opened[KeyLen : KeyLen+BlobTagLen]),
			SaltTag: BlobTag(opened[KeyLen+BlobTagLen:]),
			Tagged:  true,
		}, nil
	default:
		return BlobKey{}, ErrOpen
	}
}

// WrapRegistration seals an OPAQUE registration under a key from the erase salt, so once the blob is erased no copy
// of the record tests a passphrase (spec section 5.2).
func WrapRegistration(salt *EraseSalt, locator Locator, registration []byte) []byte {
	key := subkey(Key(*salt), locator, infoRegister)
	defer clear(key[:])
	return seal(key, registration, locator, labelRegistration)
}

func UnwrapRegistration(salt *EraseSalt, locator Locator, wrapped []byte) ([]byte, error) {
	key := subkey(Key(*salt), locator, infoRegister)
	defer clear(key[:])
	opened, err := open(key, wrapped, locator, labelRegistration)
	if err != nil || len(opened) != RegistrationLen {
		clear(opened)
		return nil, ErrOpen
	}
	return opened, nil
}

// Record pads the fields so that every sealed record is a multiple of RecordBlock bytes long.
func Record(key Key, locator Locator, fields []byte) []byte {
	padded := RecordBlock * ((len(fields) + nonceLen + tagLen + RecordBlock - 1) / RecordBlock)
	plaintext := make([]byte, padded-nonceLen-tagLen)
	copy(plaintext, fields)
	return seal(subkey(key, locator, infoFields), plaintext, locator, labelRecord)
}

func OpenRecord(key Key, locator Locator, sealed []byte) ([]byte, error) {
	return open(subkey(key, locator, infoFields), sealed, locator, labelRecord)
}

// subkey gives each use of a key its own key for one locator (spec section 5.2).
func subkey(key Key, locator Locator, info string) Key {
	derived, err := hkdf.Key(sha256.New, key[:], locator[:], info, KeyLen)
	if err != nil {
		panic(err)
	}
	defer clear(derived)
	return Key(derived)
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
	defer clear(derived)
	//nolint:gosec // G407: every blob has its own random key and salt, so its counter may start at zero.
	return cipher.NewCTR(newBlock(Key(derived)), make([]byte, aes.BlockSize))
}

// BlobMAC starts the tag of a stored blob with its salt; the caller writes the encrypted bytes after the erase block.
func BlobMAC(blobKey Key, salt EraseSalt) hash.Hash {
	derived, err := hkdf.Key(sha256.New, blobKey[:], salt[:], infoBlobTag, KeyLen)
	if err != nil {
		panic(err)
	}
	defer clear(derived)
	mac := hmac.New(sha256.New, derived)
	mac.Write(salt[:])
	return mac
}

func CheckBlobTag(mac hash.Hash, tag *BlobTag) error {
	if !hmac.Equal(mac.Sum(nil), tag[:]) {
		return ErrBlobChanged
	}
	return nil
}

// SaltTagOf lets a burn trust the salt of a blob without reading the rest of it.
func SaltTagOf(blobKey Key, salt EraseSalt) BlobTag {
	derived, err := hkdf.Key(sha256.New, blobKey[:], nil, infoSaltTag, KeyLen)
	if err != nil {
		panic(err)
	}
	defer clear(derived)
	mac := hmac.New(sha256.New, derived)
	mac.Write(salt[:])
	return BlobTag(mac.Sum(nil))
}

func CheckSaltTag(blob *BlobKey, salt EraseSalt) error {
	if !blob.Tagged {
		return nil
	}
	tag := SaltTagOf(blob.Key, salt)
	if !hmac.Equal(tag[:], blob.SaltTag[:]) {
		return ErrBlobChanged
	}
	return nil
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
