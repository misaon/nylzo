package core

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"
)

type KDF uint8

const (
	KDFNone   KDF = 0
	KDFScrypt KDF = 1
	KDFPBKDF2 KDF = 2
)

const (
	ScryptN          = 1 << 17
	ScryptR          = 8
	ScryptP          = 1
	PBKDF2Iterations = 600000
)

var ErrKDF = errors.New("core: unknown kdf profile")

func (k KDF) Valid() bool {
	return k <= KDFPBKDF2
}

func (k KDF) RequiresPassphrase() bool {
	return k != KDFNone
}

func ParseKDF(s string) (KDF, error) {
	switch s {
	case "0":
		return KDFNone, nil
	case "1":
		return KDFScrypt, nil
	case "2":
		return KDFPBKDF2, nil
	}
	return 0, ErrKDF
}

func (k KDF) String() string {
	return fmt.Sprintf("%d", uint8(k))
}

func NormalizePassphrase(passphrase string) []byte {
	return []byte(norm.NFC.String(passphrase))
}

func DerivePassphraseKey(kdf KDF, passphrase string, salt *[32]byte) ([]byte, error) {
	switch kdf {
	case KDFNone:
		return nil, nil
	case KDFScrypt:
		return scrypt.Key(NormalizePassphrase(passphrase), salt[:], ScryptN, ScryptR, ScryptP, KeyLen)
	case KDFPBKDF2:
		return pbkdf2.Key(sha256.New, string(NormalizePassphrase(passphrase)), salt[:], PBKDF2Iterations, KeyLen)
	}
	return nil, ErrKDF
}
