package core

import (
	"errors"
	"strconv"
)

type KDF uint8

const (
	KDFNone    KDF = 0
	KDFScrypt  KDF = 1
	KDFRetired KDF = 2
)

var (
	ErrKDF         = errors.New("core: unknown kdf profile")
	ErrKDFMismatch = errors.New("core: kdf profile does not match the link fragment")
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
