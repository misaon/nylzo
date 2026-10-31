package vectors

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/proofofwork"
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
	f.PoW = make([]PoW, len(powCases))
	errs := make([]error, len(powCases))
	var wait sync.WaitGroup
	for i, vector := range powCases {
		wait.Go(func() { f.PoW[i], errs[i] = powVector(vector) })
	}
	wait.Wait()
	return errors.Join(errs...)
}

func powVector(vector powCase) (PoW, error) {
	rawChallenge := deterministic("pow-challenge/"+vector.name, proofofwork.ChallengeLen)
	challenge := core.Encode(rawChallenge)
	id := deterministicID("pow/" + vector.name)
	counter := vector.counter
	if vector.solveTo != nil {
		solved, err := proofofwork.FirstSolution(
			context.Background(),
			(*[proofofwork.ChallengeLen]byte)(rawChallenge),
			id,
			*vector.solveTo,
		)
		if err != nil {
			return PoW{}, err
		}
		counter = solved
	}
	digest, err := proofofwork.Digest((*[proofofwork.ChallengeLen]byte)(rawChallenge), id, counter)
	if err != nil {
		return PoW{}, err
	}
	zeroBits := proofofwork.LeadingZeroBits(digest[:])
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
		Token:      proofofwork.Token(challenge, counter),
		Difficulty: vector.solveTo,
	}, nil
}
