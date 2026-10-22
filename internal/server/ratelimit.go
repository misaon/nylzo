package server

import (
	"context"
	"encoding/hex"
	"net/netip"
	"sync"
	"time"

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

func (s *Server) checkLimits(ctx context.Context, checks []limitCheck) (time.Duration, []store.RateCount, error) {
	hits := make([]store.RateHit, len(checks))
	for i, c := range checks {
		hits[i] = c.hit
	}
	counts, err := s.store.RateHit(ctx, hits)
	if err != nil {
		return 0, nil, err
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
	return retry, counts, nil
}

type createVerdict struct {
	retry     time.Duration
	capped    bool
	powNeeded bool
}

func (s *Server) limitCreate(ctx context.Context, now time.Time, addr netip.Addr, size int64) (createVerdict, error) {
	fine, coarse := s.bucketKeys(now, addr)
	h, d := hourWindow(now), dayWindow(now)
	r := s.cfg.Rates
	m := r.CoarseMultiplier
	checks := []limitCheck{
		{hit: store.RateHit{Key: "create:h:" + fine, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.CreatePerHour, end: h.end},
		{hit: store.RateHit{Key: "create:h:c:" + coarse, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.CreatePerHour * m, end: h.end},
		{hit: store.RateHit{Key: "create:d:" + fine, WindowStart: d.start, PurgeAt: d.end.Add(24 * time.Hour), Bytes: size}, limit: r.CreatePerDay, bytes: r.CreateBytesPerDay, end: d.end},
		{hit: store.RateHit{Key: "create:d:c:" + coarse, WindowStart: d.start, PurgeAt: d.end.Add(24 * time.Hour), Bytes: size}, limit: r.CreatePerDay * m, bytes: r.CreateBytesPerDay * m, end: d.end},
		{hit: store.RateHit{Key: "global:d", WindowStart: d.start, PurgeAt: d.end.Add(24 * time.Hour)}, end: d.end},
	}
	retry, counts, err := s.checkLimits(ctx, checks)
	if err != nil {
		return createVerdict{}, err
	}
	count := counts[4].Count
	s.globalCount.Store(count)
	v := createVerdict{retry: retry}
	if cap := r.GlobalDailyCreates; cap > 0 {
		v.capped = count > cap
		v.powNeeded = (count-1)*2 >= cap
	}
	return v, nil
}

func (s *Server) limitRead(ctx context.Context, now time.Time, addr netip.Addr) (time.Duration, error) {
	fine, coarse := s.bucketKeys(now, addr)
	h := hourWindow(now)
	r := s.cfg.Rates
	retry, _, err := s.checkLimits(ctx, []limitCheck{
		{hit: store.RateHit{Key: "read:h:" + fine, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.ReadPerHour, end: h.end},
		{hit: store.RateHit{Key: "read:h:c:" + coarse, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.ReadPerHour * r.CoarseMultiplier, end: h.end},
	})
	return retry, err
}

func (s *Server) limitAbuse(ctx context.Context, now time.Time, addr netip.Addr) (time.Duration, error) {
	fine, coarse := s.bucketKeys(now, addr)
	h := hourWindow(now)
	r := s.cfg.Rates
	retry, _, err := s.checkLimits(ctx, []limitCheck{
		{hit: store.RateHit{Key: "abuse:h:" + fine, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.AbusePerHour, end: h.end},
		{hit: store.RateHit{Key: "abuse:h:c:" + coarse, WindowStart: h.start, PurgeAt: h.end.Add(time.Hour)}, limit: r.AbusePerHour * r.CoarseMultiplier, end: h.end},
	})
	return retry, err
}

func (s *Server) powRequired() bool {
	cap := s.cfg.Rates.GlobalDailyCreates
	if cap <= 0 {
		return false
	}
	return s.globalCount.Load()*2 >= cap
}
