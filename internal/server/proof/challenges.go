package proof

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"math"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

const (
	clockSkew    = 5 * time.Second
	challengeTTL = time.Minute
	// loginTTL leaves the client of a handshake the time to stretch its passphrase before it presents the challenge.
	loginTTL             = 2 * time.Minute
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
	return c.issueFor(now, challengeTTL)
}

// IssueLogin issues the challenge that a handshake answers with, which stays open long enough for the stretching
// (spec section 6.4).
func (c *Challenges) IssueLogin(now time.Time) ([signing.ChallengeLen]byte, time.Time, bool) {
	return c.issueFor(now, loginTTL)
}

// See reports whether this process issued the challenge, no request presented it before and its window is open. A
// genuine challenge is used up before its window is checked, so whatever the answer, the process never accepts it again.
func (c *Challenges) See(challenge [signing.ChallengeLen]byte, now time.Time) bool {
	accepted, login := c.Take(challenge, now)
	if login != nil {
		login.Wipe()
	}
	return accepted
}

// Take is See that also hands over the login state of a handshake that issued the challenge (spec section 5.5).
func (c *Challenges) Take(challenge [signing.ChallengeLen]byte, now time.Time) (bool, *Login) {
	if !c.genuine(challenge) {
		return false, nil
	}
	entry, wasHeld := c.issued.take(nonce(challenge[4:challengeSignedEnd]))
	login := entry.login
	if !wasHeld || !inWindow(challenge, now, entry.lifetime) {
		if login != nil {
			login.Wipe()
		}
		return false, nil
	}
	return true, login
}

// Forget drops a challenge that its handshake did not answer with, so a refused handshake holds no place in the table.
func (c *Challenges) Forget(challenge [signing.ChallengeLen]byte) {
	entry, held := c.issued.take(nonce(challenge[4:challengeSignedEnd]))
	if held && entry.login != nil {
		entry.login.Wipe()
	}
}

// Attach puts the login state of a handshake into the challenge it answers with.
func (c *Challenges) Attach(challenge [signing.ChallengeLen]byte, login *Login) bool {
	return c.issued.attach(nonce(challenge[4:challengeSignedEnd]), login)
}

// Login is what a handshake leaves for the claim or burn that finishes it: the message, the client_mac that KE3 must
// match and the attempt that a matching KE3 takes back.
type Login struct {
	ID      core.ID
	Locator seal.Locator
	Server  opaque.ServerLogin
	Attempt store.Attempt
}

func (l *Login) Wipe() {
	l.Server.Wipe()
}

// Purge keeps the challenges whose window is open or can still open in a new table, so the old one can be erased.
func (c *Challenges) Purge(now time.Time) {
	c.issued.purgeAt(now.Unix())
}

func (c *Challenges) issueFor(now time.Time, lifetime time.Duration) ([signing.ChallengeLen]byte, time.Time, bool) {
	var challenge [signing.ChallengeLen]byte
	expires := now.Add(lifetime).Truncate(time.Second)
	binary.BigEndian.PutUint32(challenge[:4], unixSeconds(expires))
	rand.Read(challenge[4:challengeSignedEnd])
	copy(challenge[challengeSignedEnd:], c.tag(challenge[:challengeSignedEnd]))
	issued := nonce(challenge[4:challengeSignedEnd])
	return challenge, expires, c.issued.add(issued, held{expires: expires.Unix(), lifetime: lifetime}, now.Unix())
}

func (c *Challenges) tag(signed []byte) []byte {
	mac := c.ring.MAC(pepper.Challenge, signed).MAC
	return mac[:signing.ChallengeLen-challengeSignedEnd]
}

func (c *Challenges) genuine(challenge [signing.ChallengeLen]byte) bool {
	return hmac.Equal(c.tag(challenge[:challengeSignedEnd]), challenge[challengeSignedEnd:])
}

func inWindow(challenge [signing.ChallengeLen]byte, now time.Time, lifetime time.Duration) bool {
	left := int64(binary.BigEndian.Uint32(challenge[:4])) - now.Unix()
	return left >= 0 && left <= int64((lifetime+clockSkew)/time.Second)
}

// unixSeconds fills the u32be time fields of the protocol, which hold every second until 2106.
func unixSeconds(moment time.Time) uint32 {
	seconds := moment.Unix()
	if seconds < 0 || seconds > math.MaxUint32 {
		return 0
	}
	return uint32(seconds)
}
