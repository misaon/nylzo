package server

import (
	"testing"
)

func TestSpentChallengesStayBounded(t *testing.T) {
	t.Parallel()
	c := newSpentChallenges()
	c.max = 64
	for i := range c.max {
		c.seen([challengeNonceLen]byte{byte(i)}, 100, 100)
	}
	if !c.seen([challengeNonceLen]byte{1}, 100, 100) {
		t.Fatal("a remembered nonce was not reported as seen")
	}
	c.seen([challengeNonceLen]byte{0xff, 1}, 1000, 1000)
	if len(c.expires) != 1 {
		t.Fatalf("expired nonces were kept: %d", len(c.expires))
	}
	for i := range 4 * c.max {
		c.seen([challengeNonceLen]byte{byte(i), byte(i >> 8), 2}, 2000, 1000)
	}
	if len(c.expires) > c.max {
		t.Fatalf("the nonce memory grew past its bound: %d", len(c.expires))
	}
}
