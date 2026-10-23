package core

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"

	"golang.org/x/crypto/scrypt"
)

const (
	PoWN               = 1 << 14
	PoWR               = 8
	PoWP               = 1
	PoWWindowLen       = 4
	PoWNonceOffset     = PoWWindowLen
	PoWNonceLen        = 16
	PoWTagLen          = 16
	PoWChallengeLen    = PoWWindowLen + PoWNonceLen + PoWTagLen
	PoWCounterLen      = 8
	MaxPoWDifficulty   = 8
	powCounterHexChars = 2 * PoWCounterLen
)

var ErrPoWDifficulty = errors.New("core: proof-of-work difficulty out of range")

func PoWDigest(challenge *[PoWChallengeLen]byte, id ID, counter uint64) ([KeyLen]byte, error) {
	password := make([]byte, 0, PoWNonceLen+IDLen)
	password = append(password, challenge[PoWNonceOffset:PoWNonceOffset+PoWNonceLen]...)
	password = append(password, id[:]...)
	out, err := scrypt.Key(password, binary.BigEndian.AppendUint64(nil, counter), PoWN, PoWR, PoWP, KeyLen)
	if err != nil {
		return [KeyLen]byte{}, err
	}
	return [KeyLen]byte(out), nil
}

func LeadingZeroBits(b []byte) int {
	n := 0
	for _, c := range b {
		if c != 0 {
			return n + bits.LeadingZeros8(c)
		}
		n += 8
	}
	return n
}

func PoWToken(challenge string, counter uint64) string {
	return fmt.Sprintf("%s.%0*x", challenge, powCounterHexChars, counter)
}

func SolvePoW(ctx context.Context, challenge string, id ID, difficulty int) (string, uint64, error) {
	if difficulty < 0 || difficulty > MaxPoWDifficulty {
		return "", 0, ErrPoWDifficulty
	}
	raw, err := DecodeExact(challenge, PoWChallengeLen)
	if err != nil {
		return "", 0, err
	}
	chal := (*[PoWChallengeLen]byte)(raw)
	for counter := uint64(0); ; counter++ {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		digest, err := PoWDigest(chal, id, counter)
		if err != nil {
			return "", 0, err
		}
		if LeadingZeroBits(digest[:]) >= difficulty {
			return PoWToken(challenge, counter), counter, nil
		}
	}
}
