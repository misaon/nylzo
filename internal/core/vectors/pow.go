package vectors

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/misaon/nylzo/internal/core"
)

type PoW struct {
	Name       string `json:"name"`
	Challenge  string `json:"challenge"`
	ID         string `json:"id"`
	Counter    uint64 `json:"counter"`
	Digest     string `json:"digest_hex"`
	ZeroBits   int    `json:"zero_bits"`
	Token      string `json:"token"`
	Difficulty *int   `json:"difficulty,omitempty"`
}

type powCase struct {
	name        string
	counter     uint64
	solveTo     *int
	minZeroBits int
}

var powCases = []powCase{
	{name: "counter zero"},
	{name: "counter with two hex digits", counter: 0xff},
	{name: "counter above 32 bits", counter: 1<<32 + 1},
	{name: "largest safe JavaScript counter", counter: 1<<53 - 1},
	{name: "at least nine zero bits", counter: 512, minZeroBits: 9},
	{name: "first solution at difficulty 0", solveTo: new(0)},
	{name: "first solution at difficulty 4", solveTo: new(4)},
	{name: "first solution at difficulty 6", solveTo: new(6)},
}

func (f *File) pow() error {
	for _, vector := range powCases {
		pow, err := powVector(vector)
		if err != nil {
			return err
		}
		f.PoW = append(f.PoW, pow)
	}
	return nil
}

func powVector(vector powCase) (PoW, error) {
	rawChallenge := deterministic("pow-challenge/"+vector.name, core.PoWChallengeLen)
	challenge := core.Encode(rawChallenge)
	id := deterministicID("pow/" + vector.name)
	counter := vector.counter
	if vector.solveTo != nil {
		_, solved, err := core.SolvePoW(context.Background(), challenge, id, *vector.solveTo)
		if err != nil {
			return PoW{}, err
		}
		counter = solved
	}
	digest, err := core.PoWDigest((*[core.PoWChallengeLen]byte)(rawChallenge), id, counter)
	if err != nil {
		return PoW{}, err
	}
	zeroBits := core.LeadingZeroBits(digest[:])
	if zeroBits < vector.minZeroBits {
		return PoW{}, fmt.Errorf("vectors: pow case %q has fewer than %d zero bits", vector.name, vector.minZeroBits)
	}
	return PoW{
		Name:       vector.name,
		Challenge:  challenge,
		ID:         id.String(),
		Counter:    counter,
		Digest:     hex.EncodeToString(digest[:]),
		ZeroBits:   zeroBits,
		Token:      core.PoWToken(challenge, counter),
		Difficulty: vector.solveTo,
	}, nil
}
