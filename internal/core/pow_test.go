package core_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestVectorsPoW(t *testing.T) {
	f := loadVectors(t)
	if len(f.PoW) == 0 {
		t.Fatal("no proof-of-work vectors")
	}
	for _, v := range f.PoW {
		raw, err := core.DecodeExact(v.Challenge, core.PoWChallengeLen)
		if err != nil {
			t.Fatalf("%s: challenge: %v", v.Name, err)
		}
		id, err := core.ParseID(v.ID)
		if err != nil {
			t.Fatalf("%s: id: %v", v.Name, err)
		}
		digest, err := core.PoWDigest((*[core.PoWChallengeLen]byte)(raw), id, v.Counter)
		if err != nil {
			t.Fatal(err)
		}
		if got := hex.EncodeToString(digest[:]); got != v.Digest {
			t.Fatalf("%s: digest %s, want %s", v.Name, got, v.Digest)
		}
		if got := core.LeadingZeroBits(digest[:]); got != v.ZeroBits {
			t.Fatalf("%s: %d zero bits, want %d", v.Name, got, v.ZeroBits)
		}
		if got := core.PoWToken(v.Challenge, v.Counter); got != v.Token {
			t.Fatalf("%s: token %s, want %s", v.Name, got, v.Token)
		}
		if v.Difficulty == nil {
			continue
		}
		token, counter, err := core.SolvePoW(t.Context(), v.Challenge, id, *v.Difficulty)
		if err != nil || token != v.Token || counter != v.Counter {
			t.Fatalf("%s: solved %q at %d (%v), want the first solution %s", v.Name, token, counter, err, v.Token)
		}
	}
}

func TestLeadingZeroBits(t *testing.T) {
	for _, c := range []struct {
		in   []byte
		want int
	}{
		{nil, 0},
		{[]byte{0x80}, 0},
		{[]byte{0x01}, 7},
		{[]byte{0x00, 0x40}, 9},
		{[]byte{0x00, 0x00}, 16},
	} {
		if got := core.LeadingZeroBits(c.in); got != c.want {
			t.Fatalf("%x: %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSolvePoWRejectsBadInput(t *testing.T) {
	challenge := core.Encode(make([]byte, core.PoWChallengeLen))
	for _, difficulty := range []int{-1, core.MaxPoWDifficulty + 1} {
		if _, _, err := core.SolvePoW(t.Context(), challenge, core.NewID(), difficulty); !errors.Is(err, core.ErrPoWDifficulty) {
			t.Fatalf("difficulty %d: %v", difficulty, err)
		}
	}
	if _, _, err := core.SolvePoW(t.Context(), challenge[1:], core.NewID(), 0); err == nil {
		t.Fatal("a malformed challenge was accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := core.SolvePoW(ctx, challenge, core.NewID(), core.MaxPoWDifficulty); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled solve went on: %v", err)
	}
}
