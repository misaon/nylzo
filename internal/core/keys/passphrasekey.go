package keys

import (
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"

	"github.com/misaon/nylzo/internal/core"
)

const (
	ScryptN = 1 << 18
	ScryptR = 8
	ScryptP = 1
)

var ErrMalformedPassphrase = errors.New("core: passphrase is not well-formed Unicode")

// derivations admits one scrypt at a time: each one holds 256 MiB, and no caller gains from two at once.
var derivations = make(chan struct{}, 1)

func NormalizePassphrase(passphrase string) []byte {
	return []byte(norm.NFC.String(passphrase))
}

func DerivePassphraseKey(kdf core.KDF, passphrase string, salt *[32]byte) ([]byte, error) {
	if err := checkPassphrase(kdf, passphrase); err != nil {
		return nil, err
	}
	if kdf == core.KDFNone {
		return nil, nil
	}
	if kdf != core.KDFScrypt {
		return nil, core.ErrKDF
	}
	normalized := NormalizePassphrase(passphrase)
	defer clear(normalized)
	derivations <- struct{}{}
	defer func() { <-derivations }()
	return scrypt.Key(normalized, salt[:], ScryptN, ScryptR, ScryptP, core.KeyLen)
}

func checkPassphrase(kdf core.KDF, passphrase string) error {
	if kdf != core.KDFNone && !utf8.ValidString(passphrase) {
		return ErrMalformedPassphrase
	}
	return nil
}
