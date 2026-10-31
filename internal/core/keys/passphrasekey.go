package keys

import (
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"

	"github.com/misaon/nylzo/internal/core"
)

const (
	ScryptN = 1 << 17
	ScryptR = 8
	ScryptP = 1
)

var ErrMalformedPassphrase = errors.New("core: passphrase is not well-formed Unicode")

func NormalizePassphrase(passphrase string) []byte {
	return []byte(norm.NFC.String(passphrase))
}

func DerivePassphraseKey(kdf core.KDF, passphrase string, salt *[32]byte) ([]byte, error) {
	if err := checkPassphrase(kdf, passphrase); err != nil {
		return nil, err
	}
	switch kdf {
	case core.KDFNone:
		return nil, nil
	case core.KDFScrypt:
		normalized := NormalizePassphrase(passphrase)
		defer clear(normalized)
		return scrypt.Key(normalized, salt[:], ScryptN, ScryptR, ScryptP, core.KeyLen)
	case core.KDFRetired:
		return nil, core.ErrKDF
	default:
		return nil, core.ErrKDF
	}
}

func checkPassphrase(kdf core.KDF, passphrase string) error {
	if kdf != core.KDFNone && !utf8.ValidString(passphrase) {
		return ErrMalformedPassphrase
	}
	return nil
}
