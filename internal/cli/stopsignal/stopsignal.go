// Package stopsignal lets a command catch a stop signal, clean up, and then end the process by that signal, so that a
// shell waiting for nylzo sees the signal and stops its script (https://www.cons.org/cracauer/sigint.html).
package stopsignal

import (
	"context"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
)

const signalExitBase = 128

// interruptIgnored is read before anything registers Ctrl-C, since a registration clears the ignore that a shell
// sets for a background job.
var interruptIgnored = signal.Ignored(os.Interrupt)

// Notify relays signals to relay as signal.Notify does, except Ctrl-C in a process that started with it ignored.
func Notify(relay chan<- os.Signal, signals ...os.Signal) {
	if interruptIgnored {
		signals = slices.DeleteFunc(slices.Clone(signals), func(s os.Signal) bool { return s == os.Interrupt })
	}
	// signal.Notify without signals would relay every signal.
	if len(signals) > 0 {
		signal.Notify(relay, signals...)
	}
}

// StopError is the cause of a command's context that a stop signal cancelled. Like signal.NotifyContext's, it names
// the signal and matches context.Canceled.
type StopError struct {
	Signal os.Signal
}

func (e StopError) Error() string {
	return e.Signal.String() + " signal received"
}

func (StopError) Is(target error) bool {
	return target == context.Canceled
}

// Pending keeps the stop signal that the process ends by after a command.
type Pending struct {
	mutex    sync.Mutex
	received os.Signal
	held     bool
}

// Keep records a stop signal that cancelled a command. The first one counts, but Ctrl-C replaces another: a shell
// stops its script only when the command it waited for ended by Ctrl-C. A nil Pending keeps nothing.
func (p *Pending) Keep(received os.Signal) {
	p.record(received, false)
}

// Hold records a stop signal that a command held back until its work was done, so the process ends by it even when
// the command succeeded.
func (p *Pending) Hold(received os.Signal) {
	p.record(received, true)
}

func (p *Pending) Signal() os.Signal {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.received
}

func (p *Pending) Held() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.held
}

func (p *Pending) record(received os.Signal, held bool) {
	if p == nil {
		return
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.received == nil || received == os.Interrupt {
		p.received = received
	}
	p.held = p.held || held
}

// exitCode is the status a shell reports for a process that received ended: 128 plus the signal number.
func exitCode(received os.Signal) int {
	number, _ := received.(syscall.Signal)
	return signalExitBase + int(number)
}
