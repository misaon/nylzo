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

type sourceLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	max     int
	buckets map[[pepper.BucketLen]byte]*tokenBucket
}

const maxSources = 1 << 16

func newSourceLimiter(rate, burst float64) *sourceLimiter {
	return &sourceLimiter{rate: rate, burst: burst, max: maxSources, buckets: map[[pepper.BucketLen]byte]*tokenBucket{}}
}

func (l *sourceLimiter) allow(key [pepper.BucketLen]byte, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.max {
			l.evict(now)
		}
		b = newTokenBucket(l.rate, l.burst, now)
		l.buckets[key] = b
	}
	return b.allow(now)
}

func (l *sourceLimiter) evict(now time.Time) {
	refill := time.Duration(l.burst / l.rate * float64(time.Second))
	for k, b := range l.buckets {
		if now.Sub(b.last) >= refill {
			delete(l.buckets, k)
		}
	}
	for k := range l.buckets {
		if len(l.buckets) < l.max-l.max/8 {
			return
		}
		delete(l.buckets, k)
	}
}

type window struct {
	start time.Time
	end   time.Time
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
	hit   store.RateHit
	limit int64
	bytes int64
	end   time.Time
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
	now := s.clock()
	var retry time.Duration
	for i, c := range checks {
		over := c.limit > 0 && counts[i].Count > c.limit
		if c.bytes > 0 && counts[i].Bytes > c.bytes {
			over = true
		}
		if over {
			if until := c.end.Sub(now); until > retry {
				retry = until
			}
		}
	}
	return retry, nil
}

const globalCreatesKey = "global:d"

func (s *Server) limitCreate(ctx context.Context, now time.Time, addr netip.Addr, size int64) (time.Duration, error) {
	fine, coarse := s.bucketKeys(now, addr)
	h, d := hourWindow(now), dayWindow(now)
	r := s.cfg.Rates
	m := r.CoarseMultiplier
	checks := []limitCheck{
		{hit: store.RateHit{Key: "create:h:" + fine, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.CreatePerHour, end: h.end},
		{hit: store.RateHit{Key: "create:h:c:" + coarse, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.CreatePerHour * m, end: h.end},
		{hit: store.RateHit{Key: "create:d:" + fine, WindowStart: d.start, PurgeAt: d.end.Add(24 * time.Hour), Bytes: size}, limit: r.CreatePerDay, bytes: r.CreateBytesPerDay, end: d.end},
		{hit: store.RateHit{Key: "create:d:c:" + coarse, WindowStart: d.start, PurgeAt: d.end.Add(24 * time.Hour), Bytes: size}, limit: r.CreatePerDay * m, bytes: r.CreateBytesPerDay * m, end: d.end},
	}
	return s.checkLimits(ctx, checks)
}

type globalState struct {
	capped    bool
	powNeeded bool
}

func (s *Server) globalCreates(ctx context.Context, now time.Time) (globalState, error) {
	limit := s.cfg.Rates.GlobalDailyCreates
	if limit <= 0 {
		return globalState{}, nil
	}
	c, err := s.store.RateGet(ctx, globalCreatesKey, dayWindow(now).start)
	if err != nil {
		return globalState{}, err
	}
	return globalState{capped: c.Count >= limit, powNeeded: c.Count*2 >= limit}, nil
}

type globalSlot struct {
	reserved  bool
	full      bool
	powNeeded bool
}

func (s *Server) reserveGlobalCreate(ctx context.Context, now time.Time) (globalSlot, error) {
	limit := s.cfg.Rates.GlobalDailyCreates
	if limit <= 0 {
		return globalSlot{}, nil
	}
	d := dayWindow(now)
	count, ok, err := s.store.RateReserve(ctx, store.RateHit{Key: globalCreatesKey, WindowStart: d.start, PurgeAt: d.end.Add(24 * time.Hour)}, limit)
	if err != nil || !ok {
		return globalSlot{full: err == nil}, err
	}
	return globalSlot{reserved: true, powNeeded: (count-1)*2 >= limit}, nil
}

func (s *Server) releaseGlobalCreate(ctx context.Context, now time.Time) error {
	return s.store.RateRelease(ctx, globalCreatesKey, dayWindow(now).start)
}

func (s *Server) limitRead(ctx context.Context, now time.Time, addr netip.Addr) (time.Duration, error) {
	fine, coarse := s.bucketKeys(now, addr)
	h := hourWindow(now)
	r := s.cfg.Rates
	return s.checkLimits(ctx, []limitCheck{
		{hit: store.RateHit{Key: "read:h:" + fine, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.ReadPerHour, end: h.end},
		{hit: store.RateHit{Key: "read:h:c:" + coarse, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.ReadPerHour * r.CoarseMultiplier, end: h.end},
	})
}

func (s *Server) limitAbuse(ctx context.Context, now time.Time, addr netip.Addr) (time.Duration, error) {
	fine, coarse := s.bucketKeys(now, addr)
	h := hourWindow(now)
	r := s.cfg.Rates
	return s.checkLimits(ctx, []limitCheck{
		{hit: store.RateHit{Key: "abuse:h:" + fine, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.AbusePerHour, end: h.end},
		{hit: store.RateHit{Key: "abuse:h:c:" + coarse, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.AbusePerHour * r.CoarseMultiplier, end: h.end},
	})
}
