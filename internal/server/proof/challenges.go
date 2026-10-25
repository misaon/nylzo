package proof

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"math"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

const (
	clockSkew            = 5 * time.Second
	challengeTTL         = time.Minute
	challengeNonceLength = 16
	challengeSignedEnd   = 4 + challengeNonceLength
	challengeSpentKey    = "challenge:"
)

type nonce [challengeNonceLength]byte

type Challenges struct {
	ring    *pepper.Ring
	seen    *seenNonces
	batches *spendBatcher
}

func NewChallenges(ring *pepper.Ring, counters store.Counters) *Challenges {
	return &Challenges{ring: ring, seen: newSeenNonces(), batches: newSpendBatcher(counters)}
}

func (c *Challenges) Issue(now time.Time) ([core.ChallengeLen]byte, time.Time) {
	var challenge [core.ChallengeLen]byte
	expires := now.Add(challengeTTL).Truncate(time.Second)
	binary.BigEndian.PutUint32(challenge[:4], unixSeconds(expires))
	rand.Read(challenge[4:challengeSignedEnd])
	copy(challenge[challengeSignedEnd:], c.tag(challenge[:challengeSignedEnd]))
	return challenge, expires
}

// Spend reports whether this request is the first to present the challenge; a failed write refuses it for good.
func (c *Challenges) Spend(ctx context.Context, challenge [core.ChallengeLen]byte, now time.Time) (bool, error) {
	if !c.valid(challenge, now) {
		return false, nil
	}
	expires := int64(binary.BigEndian.Uint32(challenge[:4]))
	if c.seen.remember(nonce(challenge[4:challengeSignedEnd]), expires, now.Unix()) {
		return false, nil
	}
	return c.batches.reserve(ctx, &store.RateHit{
		Key:         challengeSpentKey + hex.EncodeToString(challenge[4:challengeSignedEnd]),
		WindowStart: time.Unix(0, 0),
		PurgeAt:     store.CeilMinute(time.Unix(expires, 0).Add(challengeTTL)),
		Limit:       1,
	})
}

func (c *Challenges) tag(signed []byte) []byte {
	mac, _ := c.ring.MAC(pepper.Challenge, signed)
	return mac[:core.ChallengeLen-challengeSignedEnd]
}

func (c *Challenges) valid(challenge [core.ChallengeLen]byte, now time.Time) bool {
	tagOK := hmac.Equal(c.tag(challenge[:challengeSignedEnd]), challenge[challengeSignedEnd:])
	left := int64(binary.BigEndian.Uint32(challenge[:4])) - now.Unix()
	return tagOK && left >= 0 && left <= int64((challengeTTL+clockSkew)/time.Second)
}

// unixSeconds fills the u32be time fields of the protocol, which hold every second until 2106.
func unixSeconds(t time.Time) uint32 {
	seconds := t.Unix()
	if seconds < 0 || seconds > math.MaxUint32 {
		return 0
	}
	return uint32(seconds)
}
