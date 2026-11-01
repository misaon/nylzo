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

// Limiter keeps a token bucket per key; it records the last refill to the second only (A4-3).
type Limiter struct {
	mutex      sync.Mutex
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
	l.mutex.Lock()
	defer l.mutex.Unlock()
	second := l.second(now)
	bucket, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxEntries {
			l.trim(second)
		}
		bucket = tokenBucket{tokens: l.burst, last: second}
	}
	bucket = l.refilled(bucket, second)
	allowed := bucket.tokens >= 1
	if allowed {
		bucket.tokens--
	}
	l.buckets[key] = bucket
	return allowed
}

// Purge drops every bucket that has refilled to its burst, since an absent bucket allows the same.
func (l *Limiter) Purge(now time.Time) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	second := l.second(now)
	maps.DeleteFunc(l.buckets, func(_ Bucket, bucket tokenBucket) bool {
		return l.refilled(bucket, second).tokens >= l.burst
	})
}

func (l *Limiter) Reset() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	clear(l.buckets)
}

func (l *Limiter) second(now time.Time) int64 {
	return int64(now.Sub(l.base) / time.Second)
}

func (l *Limiter) refilled(bucket tokenBucket, second int64) tokenBucket {
	if elapsed := second - bucket.last; elapsed > 0 {
		bucket.tokens = min(l.burst, bucket.tokens+float64(elapsed)*l.rate)
		bucket.last = second
	}
	return bucket
}

func (l *Limiter) trim(second int64) {
	Trim(l.buckets, l.maxEntries, func(bucket tokenBucket) bool { return l.refilled(bucket, second).tokens >= l.burst })
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
