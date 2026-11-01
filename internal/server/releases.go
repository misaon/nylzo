package server

import (
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
)

// releases keeps the record key of a message whose last lease runs until the release at lease end, or until a
// request ends the message first and cancel clears the key at once (A4-2); stop waits for releases already running.
type releases struct {
	mutex   sync.Mutex
	running sync.WaitGroup
	closed  bool
	pending map[seal.Locator]*pendingRelease
}

type pendingRelease struct {
	timer *time.Timer
	key   seal.Key
}

func newReleases() *releases {
	return &releases{pending: map[seal.Locator]*pendingRelease{}}
}

func (r *releases) schedule(locator seal.Locator, key seal.Key, delay time.Duration, release func(seal.Key)) {
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

func (r *releases) cancel(locator seal.Locator) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.forgetLocked(locator)
}

func (r *releases) forgetLocked(locator seal.Locator) {
	if pending, ok := r.pending[locator]; ok {
		pending.timer.Stop()
		clear(pending.key[:])
		delete(r.pending, locator)
	}
}

func (r *releases) stop() {
	r.mutex.Lock()
	r.closed = true
	for locator := range r.pending {
		r.forgetLocked(locator)
	}
	r.mutex.Unlock()
	r.running.Wait()
}
