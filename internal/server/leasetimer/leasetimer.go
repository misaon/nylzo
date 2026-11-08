package leasetimer

import (
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
)

// Timers keep the record key of a message whose last lease runs until the release at lease end, or until a
// request ends the message first and Cancel clears the key at once (A4-2); Stop waits for releases already running.
type Timers struct {
	mutex   sync.Mutex
	running sync.WaitGroup
	closed  bool
	pending map[seal.Locator]*pendingRelease
}

type pendingRelease struct {
	timer *time.Timer
	key   seal.Key
}

func New() *Timers {
	return &Timers{pending: map[seal.Locator]*pendingRelease{}}
}

func (r *Timers) Schedule(locator seal.Locator, key seal.Key, delay time.Duration, release func(seal.Key)) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.closed {
		return
	}
	r.forgetLocked(locator)
	pending := &pendingRelease{key: key}
	pending.timer = time.AfterFunc(delay, func() {
		r.mutex.Lock()
		if r.closed || r.pending[locator] != pending {
			r.mutex.Unlock()
			return
		}
		delete(r.pending, locator)
		r.running.Add(1)
		r.mutex.Unlock()
		defer r.running.Done()
		defer clear(pending.key[:])
		secretmode.Do(func() { release(pending.key) })
	})
	r.pending[locator] = pending
}

func (r *Timers) Cancel(locator seal.Locator) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.forgetLocked(locator)
}

func (r *Timers) Stop() {
	r.mutex.Lock()
	r.closed = true
	for locator := range r.pending {
		r.forgetLocked(locator)
	}
	r.mutex.Unlock()
	r.running.Wait()
}

func (r *Timers) forgetLocked(locator seal.Locator) {
	if pending, ok := r.pending[locator]; ok {
		pending.timer.Stop()
		clear(pending.key[:])
		delete(r.pending, locator)
	}
}
