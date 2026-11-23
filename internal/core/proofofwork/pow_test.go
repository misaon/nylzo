package proofofwork_test

import (
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/proofofwork"
)

func TestADigestOfZeroBytesHasAllItsBitsZero(t *testing.T) {
	t.Parallel()
	if got := proofofwork.LeadingZeroBits(make([]byte, 4)); got != 32 {
		t.Fatalf("got %d leading zero bits, want 32", got)
	}
}

func TestDifficultyZeroIsSolvedByTheFirstCounter(t *testing.T) {
	t.Parallel()
	challenge := core.Encode(make([]byte, proofofwork.ChallengeLen))
	token, err := proofofwork.Solve(t.Context(), challenge, core.NewID(), 0)
	if err != nil || token != challenge+"."+strings.Repeat("0", 2*proofofwork.CounterLen) {
		t.Fatalf("got %q, %v", token, err)
	}
}
