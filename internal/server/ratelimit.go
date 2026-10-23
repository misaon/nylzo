package server

import (
	"context"
	"maps"
	"net/netip"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

var singleKey bucket

type keyedLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	max     int
	base    time.Time
	buckets map[bucket]keyedBucket
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
	return &keyedLimiter{rate: rate, burst: burst, max: maxKeyedBuckets, base: time.Now(), buckets: map[bucket]keyedBucket{}}
}

func (l *keyedLimiter) allow(key bucket, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	at := int64(now.Sub(l.base))
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
	refill := l.burst / l.rate * float64(time.Second)
	evictTo(l.buckets, l.max, func(b keyedBucket) bool { return float64(at-b.last) >= refill })
}

func evictTo[K comparable, V any](m map[K]V, limit int, expired func(V) bool) {
	maps.DeleteFunc(m, func(_ K, v V) bool { return expired(v) })
	for k := range m {
		if len(m) < limit-limit/evictDivisor {
			return
		}
		delete(m, k)
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

const (
	globalCreatesKey = "global:d"
	globalStoredKey  = "global:stored:d"
)

func (s *Server) limitCreate(now time.Time, addr netip.Addr, size int64) time.Duration {
	fine, coarse := s.addrKey.bucket(addr, false), s.addrKey.bucket(addr, true)
	h, d := hourWindow(now), dayWindow(now)
	r := s.cfg.Rates
	m := r.CoarseMultiplier
	return s.counters.hit([]addrHit{
		{kind: "create:h", bucket: fine, window: h, limit: r.CreatePerHour},
		{kind: "create:d", bucket: fine, window: d, bytes: size, limit: r.CreatePerDay, bytesLimit: r.CreateBytesPerDay},
		{kind: "create:h:c", bucket: coarse, window: h, limit: r.CreatePerHour * m},
		{kind: "create:d:c", bucket: coarse, window: d, bytes: size, limit: r.CreatePerDay * m, bytesLimit: r.CreateBytesPerDay * m},
	}, now)
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
	return s.store.RateReserve(context.WithoutCancel(ctx), store.RateHit{Key: globalCreatesKey, WindowStart: d.start, PurgeAt: d.purgeAt(), Limit: limit})
}

func (s *Server) releaseGlobalCreate(ctx context.Context, now time.Time) {
	if s.cfg.Rates.GlobalDailyCreates <= 0 {
		return
	}
	if err := s.store.RateRelease(context.WithoutCancel(ctx), globalCreatesKey, dayWindow(now).start); err != nil {
		s.log.Error("global-release", "err", ErrorText(err))
	}
}

func (s *Server) storedCreateCounters(now time.Time) []store.RateHit {
	if s.cfg.Rates.GlobalDailyCreates <= 0 {
		return nil
	}
	d := dayWindow(now)
	return []store.RateHit{{Key: globalStoredKey, WindowStart: d.start, PurgeAt: d.purgeAt()}}
}

func (s *Server) limitHourly(now time.Time, addr netip.Addr, kind string, limit int64) time.Duration {
	h := hourWindow(now)
	return s.counters.hit([]addrHit{
		{kind: kind + ":h", bucket: s.addrKey.bucket(addr, false), window: h, limit: limit},
		{kind: kind + ":h:c", bucket: s.addrKey.bucket(addr, true), window: h, limit: limit * s.cfg.Rates.CoarseMultiplier},
	}, now)
}
