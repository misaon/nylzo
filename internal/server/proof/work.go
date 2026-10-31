package proof

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

const (
	workWindow   = 120 * time.Second
	workNonceKey = "pow:"
	workNonceTTL = 2 * workWindow
	workSlotWait = 10 * time.Second
	workSigned   = proofofwork.PoWNonceOffset + proofofwork.PoWNonceLen
)

var (
	ErrWork     = errors.New("server: invalid proof of work")
	ErrWorkBusy = errors.New("server: proof-of-work verifier is saturated")
)

type Work struct {
	ring       *pepper.Ring
	counters   store.Counters
	slots      chan struct{}
	slotWait   time.Duration
	difficulty int
}

func NewWork(ring *pepper.Ring, counters store.Counters, concurrency, difficulty int) *Work {
	return &Work{
		ring:       ring,
		counters:   counters,
		slots:      make(chan struct{}, concurrency),
		slotWait:   workSlotWait,
		difficulty: difficulty,
	}
}

func (w *Work) Challenge(now time.Time) string {
	challenge := make([]byte, proofofwork.PoWChallengeLen)
	binary.BigEndian.PutUint32(challenge[:proofofwork.PoWWindowLen], unixSeconds(now))
	rand.Read(challenge[proofofwork.PoWNonceOffset:workSigned])
	copy(challenge[workSigned:], w.tag(challenge[:workSigned]))
	return core.Encode(challenge)
}

type workToken struct {
	challenge *[proofofwork.PoWChallengeLen]byte
	counter   uint64
}

// Verify spends the challenge before it waits for a slot and runs scrypt, so a token works at most once.
func (w *Work) Verify(ctx context.Context, encoded string, id core.ID, now time.Time) error {
	token, ok := w.parse(encoded, now)
	if !ok {
		return ErrWork
	}
	if err := w.spend(ctx, token.challenge); err != nil {
		return err
	}
	wait := time.NewTimer(w.slotWait)
	defer wait.Stop()
	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	case <-wait.C:
		return ErrWorkBusy
	case <-ctx.Done():
		return ctx.Err()
	}
	if !token.satisfies(id, w.difficulty) {
		return ErrWork
	}
	return nil
}

func (w *Work) parse(encoded string, now time.Time) (workToken, bool) {
	encodedChallenge, encodedCounter, ok := strings.Cut(encoded, ".")
	if !ok || len(encodedCounter) != 2*proofofwork.PoWCounterLen || strings.ContainsAny(encodedCounter, "ABCDEF") {
		return workToken{}, false
	}
	decoded, err := core.DecodeExact(encodedChallenge, proofofwork.PoWChallengeLen)
	if err != nil {
		return workToken{}, false
	}
	counter, err := hex.DecodeString(encodedCounter)
	if err != nil {
		return workToken{}, false
	}
	challenge := (*[proofofwork.PoWChallengeLen]byte)(decoded)
	if !hmac.Equal(w.tag(challenge[:workSigned]), challenge[workSigned:]) {
		return workToken{}, false
	}
	age := now.Unix() - int64(binary.BigEndian.Uint32(challenge[:proofofwork.PoWWindowLen]))
	if age < -int64(clockSkew/time.Second) || age > int64(workWindow/time.Second) {
		return workToken{}, false
	}
	return workToken{challenge: challenge, counter: binary.BigEndian.Uint64(counter)}, true
}

func (w *Work) spend(ctx context.Context, challenge *[proofofwork.PoWChallengeLen]byte) error {
	issued := time.Unix(int64(binary.BigEndian.Uint32(challenge[:proofofwork.PoWWindowLen])), 0)
	used, err := w.counters.RateHit(ctx, []store.RateHit{{
		Key:         workNonceKey + hex.EncodeToString(challenge[proofofwork.PoWNonceOffset:workSigned]),
		WindowStart: time.Unix(0, 0),
		PurgeAt:     store.CeilMinute(issued.Add(workNonceTTL)),
	}})
	if err != nil {
		return err
	}
	if used[0].Count > 1 {
		return ErrWork
	}
	return nil
}

func (w *Work) tag(signed []byte) []byte {
	mac := w.ring.MAC(pepper.PoW, signed).MAC
	return mac[:proofofwork.PoWTagLen]
}

func (t workToken) satisfies(id core.ID, difficulty int) bool {
	digest, err := proofofwork.PoWDigest(t.challenge, id, t.counter)
	return err == nil && proofofwork.LeadingZeroBits(digest[:]) >= difficulty
}
