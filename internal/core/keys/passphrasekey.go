package keys

import (
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/opaque"
)

const (
	scryptN       = 1 << 18
	scryptR       = 8
	scryptP       = 1
	stretchSalt   = 16
	opaqueContext = "nylzo/v3/opaque"
)

// MaxPassphraseBytes leaves room for salt_pw in the OPRF input (spec section 2).
const MaxPassphraseBytes = opaque.MaxInputLen - PassphraseSaltLen

var (
	ErrMalformedPassphrase = errors.New("core: passphrase is not well-formed Unicode")
	ErrLongPassphrase      = errors.New("core: passphrase is longer than 65503 bytes after normalization")
)

// derivations admits one scrypt at a time: each one holds 256 MiB, and no caller gains from two at once.
var derivations = make(chan struct{}, 1)

func NormalizePassphrase(passphrase string) []byte {
	return []byte(normalizedPassphrase(passphrase))
}

func normalizedPassphrase(passphrase string) string {
	return norm.NFC.String(strings.Map(spaceSeparatorToSpace, passphrase))
}

func spaceSeparatorToSpace(character rune) rune {
	if unicode.Is(unicode.Zs, character) {
		return ' '
	}
	return character
}

// Password is the OPRF input salt_pw || pw of spec section 2.1; pw is empty without a passphrase.
func Password(secret *[core.SecretLen]byte, kdf core.KDF, passphrase string) ([]byte, error) {
	if kdf != core.KDFNone && !utf8.ValidString(passphrase) {
		return nil, ErrMalformedPassphrase
	}
	if !kdf.Valid() {
		return nil, core.ErrKDF
	}
	salt := PassphraseSalt(secret)
	defer clear(salt[:])
	if kdf == core.KDFNone {
		return append([]byte(nil), salt[:]...), nil
	}
	normalized := NormalizePassphrase(passphrase)
	defer clear(normalized)
	if len(normalized) > MaxPassphraseBytes {
		return nil, ErrLongPassphrase
	}
	return slices.Concat(salt[:], normalized), nil
}

// OPAQUEConfig binds a run to the message and its profile and picks the stretching function of the profile.
func OPAQUEConfig(id core.ID, kdf core.KDF) (opaque.Config, error) {
	if !kdf.Valid() {
		return opaque.Config{}, core.ErrKDF
	}
	stretch := opaque.Identity
	if kdf == core.KDFScrypt {
		stretch = stretchScrypt
	}
	context := make([]byte, 0, len(opaqueContext)+core.IDLen+1)
	context = append(context, opaqueContext...)
	context = append(context, id[:]...)
	context = append(context, byte(kdf))
	return opaque.Config{Context: context, Stretch: stretch}, nil
}

func stretchScrypt(message []byte) ([]byte, error) {
	derivations <- struct{}{}
	defer func() { <-derivations }()
	return scrypt.Key(message, make([]byte, stretchSalt), scryptN, scryptR, scryptP, opaque.HashLen)
}
