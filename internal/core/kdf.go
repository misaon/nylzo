package core

import (
	"errors"
	"strconv"
)

type KDF uint8

const (
	KDFNone    KDF = 0
	KDFRetired KDF = 2
	KDFScrypt  KDF = 3
)

var (
	ErrKDF         = errors.New("core: unknown kdf profile")
	ErrKDFMismatch = errors.New("core: kdf profile does not match the link fragment")
)

func ParseKDF(text string) (KDF, error) {
	for _, kdf := range []KDF{KDFNone, KDFScrypt} {
		if text == kdf.String() {
			return kdf, nil
		}
	}
	return 0, ErrKDF
}

func (k KDF) Valid() bool {
	return k == KDFNone || k == KDFScrypt
}

func (k KDF) RequiresPassphrase() bool {
	return k != KDFNone
}

func (k KDF) String() string {
	return strconv.FormatUint(uint64(k), 10)
}
