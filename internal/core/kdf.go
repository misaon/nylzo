package core

import (
	"errors"
	"strconv"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"
)

type KDF uint8

const (
	KDFNone    KDF = 0
	KDFScrypt  KDF = 1
	KDFRetired KDF = 2
)

const (
	ScryptN = 1 << 17
	ScryptR = 8
	ScryptP = 1
)

var (
	ErrKDF                 = errors.New("core: unknown kdf profile")
	ErrMalformedPassphrase = errors.New("core: passphrase is not well-formed Unicode")
)

func ParseKDF(text string) (KDF, error) {
	switch text {
	case "0":
		return KDFNone, nil
	case "1":
		return KDFScrypt, nil
	default:
		return 0, ErrKDF
	}
}

func (k KDF) Valid() bool {
	return k <= KDFScrypt
}

func (k KDF) RequiresPassphrase() bool {
	return k != KDFNone
}

func (k KDF) String() string {
	return strconv.FormatUint(uint64(k), 10)
}

func NormalizePassphrase(passphrase string) []byte {
	return []byte(norm.NFC.String(passphrase))
}

func DerivePassphraseKey(kdf KDF, passphrase string, salt *[32]byte) ([]byte, error) {
	if err := checkPassphrase(kdf, passphrase); err != nil {
		return nil, err
	}
	switch kdf {
	case KDFNone:
		return nil, nil
	case KDFScrypt:
		normalized := NormalizePassphrase(passphrase)
		defer clear(normalized)
		return scrypt.Key(normalized, salt[:], ScryptN, ScryptR, ScryptP, KeyLen)
	case KDFRetired:
		return nil, ErrKDF
	default:
		return nil, ErrKDF
	}
}

func checkPassphrase(kdf KDF, passphrase string) error {
	if kdf != KDFNone && !utf8.ValidString(passphrase) {
		return ErrMalformedPassphrase
	}
	return nil
}
