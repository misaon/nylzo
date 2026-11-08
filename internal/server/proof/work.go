package proof

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"
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
	workSigned   = proofofwork.NonceOffset + proofofwork.NonceLen
)

var (
	ErrWork     = errors.New("server: invalid proof of work")
	ErrWorkBusy = errors.New("server: proof-of-work verifier is saturated")
)

type HitCounter interface {
	RateHit(ctx context.Context, hits []store.RateHit) ([]store.RateCount, error)
}

type Work struct {
	ring     *pepper.Ring
	counters HitCounter
	slots    chan struct{}
	slotWait time.Duration
}

func NewWork(ring *pepper.Ring, counters HitCounter, concurrency int) *Work {
	return &Work{
		ring:     ring,
		counters: counters,
		slots:    make(chan struct{}, concurrency),
		slotWait: workSlotWait,
	}
}

// Challenge binds the difficulty into the tag, so a token must meet the difficulty its challenge was issued with.
func (w *Work) Challenge(now time.Time, difficulty int) string {
	challenge := make([]byte, proofofwork.ChallengeLen)
	binary.BigEndian.PutUint32(challenge[:proofofwork.WindowLen], unixSeconds(now))
	rand.Read(challenge[proofofwork.NonceOffset:workSigned])
	copy(challenge[workSigned:], w.tag(challenge[:workSigned], difficulty))
	return core.Encode(challenge)
}

type workToken struct {
	challenge  *[proofofwork.ChallengeLen]byte
	counter    uint64
	difficulty int
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
	if !token.satisfies(id, token.difficulty) {
		return ErrWork
	}
	return nil
}

func (w *Work) parse(encoded string, now time.Time) (workToken, bool) {
	encodedChallenge, encodedCounter, ok := strings.Cut(encoded, ".")
	if !ok || len(encodedCounter) != 2*proofofwork.CounterLen || strings.ContainsAny(encodedCounter, "ABCDEF") {
		return workToken{}, false
	}
	decoded, err := core.DecodeExact(encodedChallenge, proofofwork.ChallengeLen)
	if err != nil {
		return workToken{}, false
	}
	counter, err := hex.DecodeString(encodedCounter)
	if err != nil {
		return workToken{}, false
	}
	challenge := (*[proofofwork.ChallengeLen]byte)(decoded)
	difficulty, issued := w.issuedDifficulty(challenge)
	if !issued {
		return workToken{}, false
	}
	age := now.Unix() - int64(binary.BigEndian.Uint32(challenge[:proofofwork.WindowLen]))
	if age < -int64(clockSkew/time.Second) || age > int64(workWindow/time.Second) {
		return workToken{}, false
	}
	return workToken{challenge: challenge, counter: binary.BigEndian.Uint64(counter), difficulty: difficulty}, true
}

// issuedDifficulty finds the difficulty whose tag the challenge carries; a challenge this server did not issue has none.
func (w *Work) issuedDifficulty(challenge *[proofofwork.ChallengeLen]byte) (int, bool) {
	for difficulty := range proofofwork.MaxDifficulty + 1 {
		if hmac.Equal(w.tag(challenge[:workSigned], difficulty), challenge[workSigned:]) {
			return difficulty, true
		}
	}
	return 0, false
}

func (w *Work) spend(ctx context.Context, challenge *[proofofwork.ChallengeLen]byte) error {
	issued := time.Unix(int64(binary.BigEndian.Uint32(challenge[:proofofwork.WindowLen])), 0)
	used, err := w.counters.RateHit(ctx, []store.RateHit{{
		Key:         workNonceKey + hex.EncodeToString(challenge[proofofwork.NonceOffset:workSigned]),
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

func (w *Work) tag(signed []byte, difficulty int) []byte {
	encoded := byte(proofofwork.MaxDifficulty)
	if difficulty >= 0 && difficulty <= proofofwork.MaxDifficulty {
		encoded = byte(difficulty)
	}
	mac := w.ring.MAC(pepper.PoW, append(slices.Clip(signed), encoded)).MAC
	return mac[:proofofwork.TagLen]
}

func (t workToken) satisfies(id core.ID, difficulty int) bool {
	digest, err := proofofwork.Digest(t.challenge, id, t.counter)
	return err == nil && proofofwork.LeadingZeroBits(digest[:]) >= difficulty
}
