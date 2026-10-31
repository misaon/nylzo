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
	ScryptN       = 1 << 14
	ScryptR       = 8
	ScryptP       = 1
	WindowLen     = 4
	NonceOffset   = WindowLen
	NonceLen      = 16
	TagLen        = 16
	ChallengeLen  = WindowLen + NonceLen + TagLen
	CounterLen    = 8
	MaxDifficulty = 8
)

var ErrDifficulty = errors.New("core: proof-of-work difficulty out of range")

func Digest(challenge *[ChallengeLen]byte, id core.ID, counter uint64) ([core.KeyLen]byte, error) {
	password := make([]byte, 0, NonceLen+core.IDLen)
	password = append(password, challenge[NonceOffset:NonceOffset+NonceLen]...)
	password = append(password, id[:]...)
	digest, err := scrypt.Key(
		password,
		binary.BigEndian.AppendUint64(nil, counter),
		ScryptN,
		ScryptR,
		ScryptP,
		core.KeyLen,
	)
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

func Token(challenge string, counter uint64) string {
	return fmt.Sprintf("%s.%0*x", challenge, 2*CounterLen, counter)
}

func Solve(ctx context.Context, challenge string, id core.ID, difficulty int) (string, error) {
	if difficulty < 0 || difficulty > MaxDifficulty {
		return "", ErrDifficulty
	}
	decoded, err := core.DecodeExact(challenge, ChallengeLen)
	if err != nil {
		return "", err
	}
	counter, err := FirstSolution(ctx, (*[ChallengeLen]byte)(decoded), id, difficulty)
	if err != nil {
		return "", err
	}
	return Token(challenge, counter), nil
}

func FirstSolution(ctx context.Context, challenge *[ChallengeLen]byte, id core.ID, difficulty int) (uint64, error) {
	for counter := uint64(0); ; counter++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		digest, err := Digest(challenge, id, counter)
		if err != nil {
			return 0, err
		}
		if LeadingZeroBits(digest[:]) >= difficulty {
			return counter, nil
		}
	}
}
