package ratelimit

import (
	"sync"
	"time"
)

// Fair is the instance token bucket. While it is less than half full, it serves each /24 or /48 at most its share,
// the instance rate divided by the ranges active in this or the last second, so many ranges that each stay within
// their own limits cannot take the bucket from a range that asks for no more than its share (F4).
type Fair struct {
	mutex          sync.Mutex
	rate           float64
	burst          float64
	tokens         float64
	refilled       time.Time
	second         int64
	served         map[Bucket]int
	rangesLastTime int
}

func NewFair(rate, burst float64) *Fair {
	return &Fair{rate: rate, burst: burst, tokens: burst, served: map[Bucket]int{}}
}

func (f *Fair) Allow(addressRange Bucket, now time.Time) bool {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.refill(now)
	if f.tokens < 1 {
		return false
	}
	if f.tokens < f.burst/2 {
		ranges := len(f.served)
		if _, seen := f.served[addressRange]; !seen {
			ranges++
		}
		if f.served[addressRange] >= max(1, int(f.rate)/max(ranges, f.rangesLastTime)) {
			return false
		}
	}
	f.tokens--
	f.served[addressRange]++
	return true
}

func (f *Fair) refill(now time.Time) {
	if second := now.Unix(); second != f.second {
		f.rangesLastTime = 0
		if second == f.second+1 {
			f.rangesLastTime = len(f.served)
		}
		f.second = second
		clear(f.served)
	}
	if elapsed := now.Sub(f.refilled).Seconds(); elapsed > 0 {
		f.tokens = min(f.burst, f.tokens+elapsed*f.rate)
		f.refilled = now
	}
}
