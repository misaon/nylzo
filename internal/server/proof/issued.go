package proof

import (
	"math"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/server/ratelimit"
)

// MinCapacity is the fewest challenges a process may hold, whatever the instance bucket.
const MinCapacity = 1 << 17

// challengeHold is the longest a challenge stays in the table: its lifetime, the second of rounding, the clock skew
// and one minute until the purge.
const challengeHold = challengeTTL + time.Second + clockSkew + time.Minute

// Capacity holds twice the challenges that an instance bucket of this rate and burst can issue while they stay held.
func Capacity(perSecond, burst float64) int {
	return int(min(math.MaxInt32, max(MinCapacity, 2*math.Ceil(perSecond*challengeHold.Seconds()+burst))))
}

// issuedNonces holds the expiry of each challenge this process issued and nobody presented yet; a nonce it no longer
// holds is refused for good, whatever the clock does.
type issuedNonces struct {
	mutex    sync.Mutex
	capacity int
	purgedAt int64
	expires  map[nonce]int64
}

func newIssuedNonces(capacity int) *issuedNonces {
	return &issuedNonces{capacity: capacity, expires: map[nonce]int64{}}
}

// add purges a full table at most once a second before it gives up, so a stalled minute purge or a clock step does
// not stop every signed request.
func (s *issuedNonces) add(issued nonce, expires, now int64) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(s.expires) >= s.capacity && now != s.purgedAt {
		s.purge(now)
	}
	if len(s.expires) >= s.capacity {
		return false
	}
	s.expires[issued] = expires
	return true
}

// take reports whether the nonce was held and stops holding it, so it is accepted once.
func (s *issuedNonces) take(presented nonce) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	_, held := s.expires[presented]
	delete(s.expires, presented)
	return held
}

func (s *issuedNonces) purgeAt(now int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.purge(now)
}

// purge keeps, in a new table, only the challenges whose window is open or can still open by this clock.
func (s *issuedNonces) purge(now int64) {
	s.purgedAt = now
	lastLive, latest := now-int64(clockSkew/time.Second), now+int64((challengeTTL+clockSkew)/time.Second)
	s.expires = ratelimit.Rebuilt(
		s.expires,
		func(expires int64) bool { return expires >= lastLive && expires <= latest },
	)
}
