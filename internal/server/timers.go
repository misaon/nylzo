package server

import (
	"sync"
	"time"
)

type timers struct {
	mu      sync.Mutex
	running sync.WaitGroup
	closed  bool
}

func (t *timers) after(d time.Duration, fn func()) {
	time.AfterFunc(d, func() {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return
		}
		t.running.Add(1)
		t.mu.Unlock()
		defer t.running.Done()
		fn()
	})
}

func (t *timers) stop() {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	t.running.Wait()
}
