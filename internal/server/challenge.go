package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

const (
	challengeTTL      = time.Minute
	challengeNonceLen = 16
	challengeSigned   = 4 + challengeNonceLen
	challengeSpentKey = "challenge:"
	maxSpentNonces    = 1 << 16
	spendBatchMax     = 256
)

type spentChallenges struct {
	mu      sync.Mutex
	max     int
	expires map[[challengeNonceLen]byte]int64
}

func newSpentChallenges() *spentChallenges {
	return &spentChallenges{max: maxSpentNonces, expires: map[[challengeNonceLen]byte]int64{}}
}

func (c *spentChallenges) seen(nonce [challengeNonceLen]byte, expires int64, now int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.expires[nonce]; ok {
		return true
	}
	if len(c.expires) >= c.max {
		c.evict(now)
	}
	c.expires[nonce] = expires
	return false
}

func (c *spentChallenges) evict(now int64) {
	evictTo(c.expires, c.max, func(exp int64) bool { return now > exp+int64(powClockSkew/time.Second) })
}

type spendBatch struct {
	hits     []store.RateHit
	turn     chan struct{}
	done     chan struct{}
	reserved []bool
	err      error
}

type spendBatcher struct {
	store   store.Store
	max     int
	mu      sync.Mutex
	writing bool
	queue   []*spendBatch
}

func newSpendBatcher(st store.Store) *spendBatcher {
	return &spendBatcher{store: st, max: spendBatchMax}
}

func (b *spendBatcher) reserve(ctx context.Context, hit store.RateHit) (bool, error) {
	b.mu.Lock()
	if n := len(b.queue); n == 0 || len(b.queue[n-1].hits) == b.max {
		b.queue = append(b.queue, &spendBatch{turn: make(chan struct{}), done: make(chan struct{})})
	}
	batch := b.queue[len(b.queue)-1]
	i := len(batch.hits)
	batch.hits = append(batch.hits, hit)
	if i == 0 && !b.writing {
		b.writing = true
		close(batch.turn)
	}
	b.mu.Unlock()
	if i == 0 {
		<-batch.turn
		b.write(ctx, batch)
	}
	select {
	case <-batch.done:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if batch.err != nil {
		return false, batch.err
	}
	return batch.reserved[i], nil
}

var errSpendAborted = errors.New("server: the challenge spend write stopped before it finished")

func (b *spendBatcher) write(ctx context.Context, batch *spendBatch) {
	b.mu.Lock()
	b.queue[0] = nil
	b.queue = b.queue[1:]
	b.mu.Unlock()
	defer b.passTurn()
	defer close(batch.done)
	defer func() {
		if recover() != nil {
			batch.reserved, batch.err = nil, errSpendAborted
		}
	}()
	batch.reserved, batch.err = b.store.RateReserveBatch(context.WithoutCancel(ctx), batch.hits)
}

func (b *spendBatcher) passTurn() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queue) > 0 {
		close(b.queue[0].turn)
		return
	}
	b.writing = false
}

type spentKey struct{}

func (s *Server) spendChallenges(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig, err := core.ParseAuthorization(r.Header.Get("Authorization"))
		if !strings.HasPrefix(r.URL.Path, "/api/") || err != nil {
			next.ServeHTTP(w, r)
			return
		}
		spent, err := s.spendChallenge(r.Context(), sig.Challenge)
		if err != nil {
			s.writeInternal(w, r, err)
			return
		}
		inner := r
		if spent {
			inner = r.WithContext(context.WithValue(r.Context(), spentKey{}, &sig.Challenge))
		}
		next.ServeHTTP(w, inner)
	})
}

func spentBy(r *http.Request, c [core.ChallengeLen]byte) bool {
	spent, ok := r.Context().Value(spentKey{}).(*[core.ChallengeLen]byte)
	return ok && *spent == c
}

type challengeResponse struct {
	Challenge string `json:"challenge"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	c, expires := s.issueChallenge(s.clock())
	writeJSON(w, http.StatusOK, challengeResponse{Challenge: core.Encode(c[:]), ExpiresAt: formatTime(expires)})
}

func (s *Server) issueChallenge(now time.Time) ([core.ChallengeLen]byte, time.Time) {
	var c [core.ChallengeLen]byte
	expires := now.Add(challengeTTL).Truncate(time.Second)
	binary.BigEndian.PutUint32(c[:4], uint32(expires.Unix()))
	rand.Read(c[4:challengeSigned])
	copy(c[challengeSigned:], s.challengeTag(c[:challengeSigned]))
	return c, expires
}

func (s *Server) challengeTag(signed []byte) []byte {
	mac, _ := s.ring.MAC(pepper.Challenge, signed)
	return mac[:core.ChallengeLen-challengeSigned]
}

func (s *Server) validChallenge(c [core.ChallengeLen]byte, now time.Time) bool {
	tagOK := hmac.Equal(s.challengeTag(c[:challengeSigned]), c[challengeSigned:])
	left := int64(binary.BigEndian.Uint32(c[:4])) - now.Unix()
	return tagOK && left >= 0 && left <= int64((challengeTTL+powClockSkew)/time.Second)
}

func (s *Server) spendChallenge(ctx context.Context, c [core.ChallengeLen]byte) (bool, error) {
	now := s.clock()
	if !s.validChallenge(c, now) {
		return false, nil
	}
	expires := int64(binary.BigEndian.Uint32(c[:4]))
	if s.spent.seen([challengeNonceLen]byte(c[4:challengeSigned]), expires, now.Unix()) {
		return false, nil
	}
	return s.spends.reserve(ctx, store.RateHit{
		Key:         challengeSpentKey + hex.EncodeToString(c[4:challengeSigned]),
		WindowStart: time.Unix(0, 0),
		PurgeAt:     store.CeilMinute(time.Unix(expires, 0).Add(challengeTTL)),
		Limit:       1,
	})
}
