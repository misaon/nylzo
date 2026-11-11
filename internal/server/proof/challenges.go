package proof

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"math"
	"time"

	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	clockSkew            = 5 * time.Second
	challengeTTL         = time.Minute
	challengeNonceLength = 16
	challengeSignedEnd   = 4 + challengeNonceLength
)

type nonce [challengeNonceLength]byte

type Challenges struct {
	ring   *pepper.Ring
	issued *issuedNonces
}

func NewChallenges(ring *pepper.Ring, capacity int) *Challenges {
	return &Challenges{ring: ring, issued: newIssuedNonces(capacity)}
}

// Issue reports false while the process already holds as many challenges as it may.
func (c *Challenges) Issue(now time.Time) ([signing.ChallengeLen]byte, time.Time, bool) {
	var challenge [signing.ChallengeLen]byte
	expires := now.Add(challengeTTL).Truncate(time.Second)
	binary.BigEndian.PutUint32(challenge[:4], unixSeconds(expires))
	rand.Read(challenge[4:challengeSignedEnd])
	copy(challenge[challengeSignedEnd:], c.tag(challenge[:challengeSignedEnd]))
	return challenge, expires, c.issued.add(nonce(challenge[4:challengeSignedEnd]), expires.Unix(), now.Unix())
}

// See reports whether this process issued the challenge, no request presented it before and its window is open. A
// genuine challenge is used up before its window is checked, so whatever the answer, the process never accepts it again.
func (c *Challenges) See(challenge [signing.ChallengeLen]byte, now time.Time) bool {
	return c.genuine(challenge) && c.issued.take(nonce(challenge[4:challengeSignedEnd])) && inWindow(challenge, now)
}

// Purge keeps the challenges whose window is open or can still open in a new table, so the old one can be erased.
func (c *Challenges) Purge(now time.Time) {
	c.issued.purgeAt(now.Unix())
}

func (c *Challenges) tag(signed []byte) []byte {
	mac := c.ring.MAC(pepper.Challenge, signed).MAC
	return mac[:signing.ChallengeLen-challengeSignedEnd]
}

func (c *Challenges) genuine(challenge [signing.ChallengeLen]byte) bool {
	return hmac.Equal(c.tag(challenge[:challengeSignedEnd]), challenge[challengeSignedEnd:])
}

func inWindow(challenge [signing.ChallengeLen]byte, now time.Time) bool {
	left := int64(binary.BigEndian.Uint32(challenge[:4])) - now.Unix()
	return left >= 0 && left <= int64((challengeTTL+clockSkew)/time.Second)
}

// unixSeconds fills the u32be time fields of the protocol, which hold every second until 2106.
func unixSeconds(moment time.Time) uint32 {
	seconds := moment.Unix()
	if seconds < 0 || seconds > math.MaxUint32 {
		return 0
	}
	return uint32(seconds)
}
