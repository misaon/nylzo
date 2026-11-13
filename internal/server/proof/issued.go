package proof

import (
	"math"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/server/ratelimit"
)

// MinCapacity is the fewest challenges a process may hold, whatever the instance bucket.
const MinCapacity = 1 << 17

// challengeHold is the longest a challenge stays in the table: the lifetime of a handshake's, the second of rounding,
// the clock skew and one minute until the purge.
const challengeHold = loginTTL + time.Second + clockSkew + time.Minute

// Capacity holds twice the challenges that an instance bucket of this rate and burst can issue while they stay held.
func Capacity(perSecond, burst float64) int {
	return int(min(math.MaxInt32, max(MinCapacity, 2*math.Ceil(perSecond*challengeHold.Seconds()+burst))))
}

// issuedNonces holds the expiry of each challenge this process issued and nobody presented yet, with the login state
// of a handshake that issued it; a nonce it no longer holds is refused for good, whatever the clock does.
type issuedNonces struct {
	mutex    sync.Mutex
	capacity int
	purgedAt int64
	held     map[nonce]held
}

type held struct {
	expires  int64
	lifetime time.Duration
	login    *Login
}

func newIssuedNonces(capacity int) *issuedNonces {
	return &issuedNonces{capacity: capacity, held: map[nonce]held{}}
}

// add purges a full table at most once a second before it gives up, so a stalled minute purge or a clock step does
// not stop every signed request.
func (s *issuedNonces) add(issued nonce, entry held, now int64) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(s.held) >= s.capacity && now != s.purgedAt {
		s.purge(now)
	}
	if len(s.held) >= s.capacity {
		return false
	}
	s.held[issued] = entry
	return true
}

// attach gives a held nonce the login state of the handshake that issued it.
func (s *issuedNonces) attach(issued nonce, login *Login) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	entry, ok := s.held[issued]
	if ok {
		entry.login = login
		s.held[issued] = entry
	}
	return ok
}

// take reports whether the nonce was held and stops holding it, so it is accepted once, with its entry.
func (s *issuedNonces) take(presented nonce) (held, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	entry, ok := s.held[presented]
	delete(s.held, presented)
	return entry, ok
}

func (s *issuedNonces) purgeAt(now int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.purge(now)
}

// purge keeps, in a new table, only the challenges whose window is open or can still open by this clock.
func (s *issuedNonces) purge(now int64) {
	s.purgedAt = now
	lastLive := now - int64(clockSkew/time.Second)
	s.held = ratelimit.Rebuilt(s.held, func(entry held) bool {
		keep := entry.expires >= lastLive && entry.expires <= now+int64((entry.lifetime+clockSkew)/time.Second)
		if !keep && entry.login != nil {
			entry.login.Wipe()
		}
		return keep
	})
}
