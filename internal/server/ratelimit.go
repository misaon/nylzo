package server

import (
	"context"
	"encoding/hex"
	"net/netip"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(rate, burst float64, now time.Time) *tokenBucket {
	return &tokenBucket{rate: rate, burst: burst, tokens: burst, last: now}
}

func (b *tokenBucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type keyedLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	max     int
	buckets map[[pepper.BucketLen]byte]keyedBucket
}

type keyedBucket struct {
	tokens float64
	last   int64
}

const (
	maxKeyedBuckets = 1 << 16
	evictDivisor    = 8
	prefixShare     = 0.5
)

func newKeyedLimiter(rate, burst float64) *keyedLimiter {
	return &keyedLimiter{rate: rate, burst: burst, max: maxKeyedBuckets, buckets: map[[pepper.BucketLen]byte]keyedBucket{}}
}

func (l *keyedLimiter) allow(key [pepper.BucketLen]byte, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	at := now.UnixNano()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.max {
			l.evict(at)
		}
		b = keyedBucket{tokens: l.burst, last: at}
	}
	if elapsed := time.Duration(at - b.last).Seconds(); elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
		b.last = at
	}
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.buckets[key] = b
	return allowed
}

func (l *keyedLimiter) evict(at int64) {
	refill := int64(l.burst / l.rate * float64(time.Second))
	for k, b := range l.buckets {
		if at-b.last >= refill {
			delete(l.buckets, k)
		}
	}
	for k := range l.buckets {
		if len(l.buckets) < l.max-l.max/evictDivisor {
			return
		}
		delete(l.buckets, k)
	}
}

type window struct {
	start time.Time
	end   time.Time
}

func (w window) purgeAt() time.Time {
	return w.end.Add(w.end.Sub(w.start))
}

func hourWindow(now time.Time) window {
	start := now.UTC().Truncate(time.Hour)
	return window{start: start, end: start.Add(time.Hour)}
}

func dayWindow(now time.Time) window {
	u := now.UTC()
	start := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	return window{start: start, end: start.Add(24 * time.Hour)}
}

type limitCheck struct {
	hit store.RateHit
	end time.Time
}

func (s *Server) bucketKeys(now time.Time, addr netip.Addr) (fine string, coarse string) {
	f := s.ring.Bucket(now, addr, false)
	c := s.ring.Bucket(now, addr, true)
	return hex.EncodeToString(f[:]), hex.EncodeToString(c[:])
}

func (s *Server) checkLimits(ctx context.Context, checks []limitCheck) (time.Duration, error) {
	hits := make([]store.RateHit, len(checks))
	for i, c := range checks {
		hits[i] = c.hit
	}
	counts, err := s.store.RateHit(ctx, hits)
	if err != nil {
		return 0, err
	}
	for i, c := range counts {
		if checks[i].hit.Over(c) {
			return checks[i].end.Sub(s.clock()), nil
		}
	}
	return 0, nil
}

const (
	globalCreatesKey = "global:d"
	globalStoredKey  = "global:stored:d"
	pendingRetry     = time.Minute
)

func (s *Server) limitCreate(ctx context.Context, now time.Time, addr netip.Addr, size int64) (time.Duration, error) {
	fine, coarse := s.bucketKeys(now, addr)
	h, d := hourWindow(now), dayWindow(now)
	r := s.cfg.Rates
	m := r.CoarseMultiplier
	checks := []limitCheck{
		{hit: store.RateHit{Key: "create:h:" + fine, WindowStart: h.start, PurgeAt: h.purgeAt(), Limit: r.CreatePerHour}, end: h.end},
		{hit: store.RateHit{Key: "create:d:" + fine, WindowStart: d.start, PurgeAt: d.purgeAt(), Bytes: size, Limit: r.CreatePerDay, BytesLimit: r.CreateBytesPerDay}, end: d.end},
		{hit: store.RateHit{Key: "create:h:c:" + coarse, WindowStart: h.start, PurgeAt: h.purgeAt(), Limit: r.CreatePerHour * m}, end: h.end},
		{hit: store.RateHit{Key: "create:d:c:" + coarse, WindowStart: d.start, PurgeAt: d.purgeAt(), Bytes: size, Limit: r.CreatePerDay * m, BytesLimit: r.CreateBytesPerDay * m}, end: d.end},
	}
	return s.checkLimits(ctx, checks)
}

type globalState struct {
	full      bool
	powNeeded bool
}

func (s *Server) globalGate(ctx context.Context, now time.Time) (globalState, error) {
	limit := s.cfg.Rates.GlobalDailyCreates
	if limit <= 0 {
		return globalState{}, nil
	}
	c, err := s.store.RateGet(ctx, globalCreatesKey, dayWindow(now).start)
	return globalState{full: c.Count >= limit, powNeeded: s.powNeededAfter(c.Count)}, err
}

func (s *Server) powNeededAfter(creates int64) bool {
	limit := s.cfg.Rates.GlobalDailyCreates
	return limit > 0 && creates*2 >= limit
}

func (s *Server) reserveGlobalCreate(ctx context.Context, now time.Time) (slot int64, ok bool, err error) {
	limit := s.cfg.Rates.GlobalDailyCreates
	if limit <= 0 {
		return 0, true, nil
	}
	d := dayWindow(now)
	return s.store.RateReserve(context.WithoutCancel(ctx), store.RateHit{Key: globalCreatesKey, WindowStart: d.start, PurgeAt: d.purgeAt()}, limit)
}

func (s *Server) releaseGlobalCreate(ctx context.Context, now time.Time) {
	if s.cfg.Rates.GlobalDailyCreates <= 0 {
		return
	}
	if err := s.store.RateRelease(context.WithoutCancel(ctx), globalCreatesKey, dayWindow(now).start); err != nil {
		s.log.Error("global-release", "err", err.Error())
	}
}

func (s *Server) countStoredCreate(ctx context.Context, now time.Time) {
	if s.cfg.Rates.GlobalDailyCreates <= 0 {
		return
	}
	d := dayWindow(now)
	if _, err := s.store.RateHit(ctx, []store.RateHit{{Key: globalStoredKey, WindowStart: d.start, PurgeAt: d.purgeAt()}}); err != nil {
		s.log.Error("global-stored", "err", err.Error())
	}
}

func (s *Server) limitHourly(ctx context.Context, now time.Time, addr netip.Addr, kind string, limit int64) (time.Duration, error) {
	fine, coarse := s.bucketKeys(now, addr)
	h := hourWindow(now)
	return s.checkLimits(ctx, []limitCheck{
		{hit: store.RateHit{Key: kind + ":h:" + fine, WindowStart: h.start, PurgeAt: h.purgeAt(), Limit: limit}, end: h.end},
		{hit: store.RateHit{Key: kind + ":h:c:" + coarse, WindowStart: h.start, PurgeAt: h.purgeAt(), Limit: limit * s.cfg.Rates.CoarseMultiplier}, end: h.end},
	})
}
