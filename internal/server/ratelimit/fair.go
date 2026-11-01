package ratelimit

import (
	"maps"
	"sync"
	"time"
)

const activeRangeWindow = 10 * time.Second

// Fair is the instance token bucket. While it is less than half full, it paces each /24 or /48 to one request per
// (ranges + 1) / rate seconds, where ranges counts those seen in the last ten seconds or still paced. A range that
// comes when its turn is due takes a token if one is left and otherwise loses the turn, so refused ranges cannot queue
// up for the spare share and take it from a range that asks for no more than its share (F4).
type Fair struct {
	mutex    sync.Mutex
	rate     float64
	burst    float64
	tokens   float64
	refilled time.Time
	pruned   time.Time
	ranges   map[Bucket]paced
}

type paced struct {
	seen time.Time
	next time.Time
}

func NewFair(rate, burst float64) *Fair {
	return &Fair{rate: rate, burst: burst, tokens: burst, ranges: map[Bucket]paced{}}
}

func (f *Fair) Allow(addressRange Bucket, now time.Time) bool {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.refill(now)
	unpressed := f.tokens >= max(1, f.burst/2)
	state, tracked := f.ranges[addressRange]
	if !tracked && len(f.ranges) >= maxTableEntries {
		return f.take(unpressed)
	}
	state.seen = now
	f.ranges[addressRange] = state
	if unpressed || now.Before(state.next) {
		return f.take(unpressed)
	}
	state.next = now.Add(time.Duration(float64(time.Second) * float64(len(f.ranges)+1) / f.rate))
	f.ranges[addressRange] = state
	return f.take(f.tokens >= 1)
}

// Purge copies the ranges that still count into a new map, which is what erases dropped keys from memory.
func (f *Fair) Purge(now time.Time) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.ranges = Rebuilt(f.ranges, func(state paced) bool { return counts(state, now) })
	f.pruned = now
}

func (f *Fair) Reset() {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.ranges = Rebuilt(f.ranges, func(paced) bool { return false })
}

func (f *Fair) take(allowed bool) bool {
	if allowed {
		f.tokens--
	}
	return allowed
}

func (f *Fair) refill(now time.Time) {
	if elapsed := now.Sub(f.refilled).Seconds(); elapsed > 0 {
		f.tokens = min(f.burst, f.tokens+elapsed*f.rate)
		f.refilled = now
	}
	if now.Sub(f.pruned) >= time.Second {
		maps.DeleteFunc(f.ranges, func(_ Bucket, state paced) bool { return !counts(state, now) })
		f.pruned = now
	}
}

func counts(state paced, now time.Time) bool {
	return now.Sub(state.seen) < activeRangeWindow || now.Before(state.next)
}
