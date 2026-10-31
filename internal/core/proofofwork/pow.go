package proofofwork

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"

	"golang.org/x/crypto/scrypt"

	"github.com/misaon/nylzo/internal/core"
)

const (
	PoWN             = 1 << 14
	PoWR             = 8
	PoWP             = 1
	PoWWindowLen     = 4
	PoWNonceOffset   = PoWWindowLen
	PoWNonceLen      = 16
	PoWTagLen        = 16
	PoWChallengeLen  = PoWWindowLen + PoWNonceLen + PoWTagLen
	PoWCounterLen    = 8
	MaxPoWDifficulty = 8
)

var ErrPoWDifficulty = errors.New("core: proof-of-work difficulty out of range")

func PoWDigest(challenge *[PoWChallengeLen]byte, id core.ID, counter uint64) ([core.KeyLen]byte, error) {
	password := make([]byte, 0, PoWNonceLen+core.IDLen)
	password = append(password, challenge[PoWNonceOffset:PoWNonceOffset+PoWNonceLen]...)
	password = append(password, id[:]...)
	digest, err := scrypt.Key(password, binary.BigEndian.AppendUint64(nil, counter), PoWN, PoWR, PoWP, core.KeyLen)
	if err != nil {
		return [core.KeyLen]byte{}, err
	}
	return [core.KeyLen]byte(digest), nil
}

func LeadingZeroBits(data []byte) int {
	zeros := 0
	for _, octet := range data {
		if octet != 0 {
			return zeros + bits.LeadingZeros8(octet)
		}
		zeros += 8
	}
	return zeros
}

func PoWToken(challenge string, counter uint64) string {
	return fmt.Sprintf("%s.%0*x", challenge, 2*PoWCounterLen, counter)
}

func SolvePoW(ctx context.Context, challenge string, id core.ID, difficulty int) (string, error) {
	if difficulty < 0 || difficulty > MaxPoWDifficulty {
		return "", ErrPoWDifficulty
	}
	decoded, err := core.DecodeExact(challenge, PoWChallengeLen)
	if err != nil {
		return "", err
	}
	counter, err := FirstSolution(ctx, (*[PoWChallengeLen]byte)(decoded), id, difficulty)
	if err != nil {
		return "", err
	}
	return PoWToken(challenge, counter), nil
}

func FirstSolution(ctx context.Context, challenge *[PoWChallengeLen]byte, id core.ID, difficulty int) (uint64, error) {
	for counter := uint64(0); ; counter++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		digest, err := PoWDigest(challenge, id, counter)
		if err != nil {
			return 0, err
		}
		if LeadingZeroBits(digest[:]) >= difficulty {
			return counter, nil
		}
	}
}
