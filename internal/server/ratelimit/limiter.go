package ratelimit

import (
	"maps"
	"sync"
	"time"
)

const (
	maxTableEntries = 1 << 16
	trimDivisor     = 8
)

type Limiter struct {
	mu         sync.Mutex
	rate       float64
	burst      float64
	maxEntries int
	base       time.Time
	buckets    map[Bucket]tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   int64
}

func NewLimiter(rate, burst float64) *Limiter {
	return &Limiter{
		rate:       rate,
		burst:      burst,
		maxEntries: maxTableEntries,
		base:       time.Now(),
		buckets:    map[Bucket]tokenBucket{},
	}
}

func (l *Limiter) Allow(key Bucket, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	sinceBase := int64(now.Sub(l.base))
	bucket, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxEntries {
			l.trim(sinceBase)
		}
		bucket = tokenBucket{tokens: l.burst, last: sinceBase}
	}
	if elapsed := time.Duration(sinceBase - bucket.last).Seconds(); elapsed > 0 {
		bucket.tokens = min(l.burst, bucket.tokens+elapsed*l.rate)
		bucket.last = sinceBase
	}
	allowed := bucket.tokens >= 1
	if allowed {
		bucket.tokens--
	}
	l.buckets[key] = bucket
	return allowed
}

func (l *Limiter) trim(sinceBase int64) {
	refill := l.burst / l.rate * float64(time.Second)
	Trim(l.buckets, l.maxEntries, func(bucket tokenBucket) bool { return float64(sinceBase-bucket.last) >= refill })
}

func Trim[K comparable, V any](table map[K]V, limit int, expired func(V) bool) {
	maps.DeleteFunc(table, func(_ K, value V) bool { return expired(value) })
	// Map iteration order picks the dropped entries at random.
	for key := range table {
		if len(table) < limit-limit/trimDivisor {
			return
		}
		delete(table, key)
	}
}
