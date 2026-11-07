// Package iolimit bounds the goroutines that hold an operating system thread in a blocking file call, so slow storage
// cannot make the runtime start more threads than the container allows.
package iolimit

import "context"

// Slots stays far below the container's thread limit, which must also cover the runtime, the network and SQLite.
const Slots = 16

var slots = make(chan struct{}, Slots)

// Acquire takes a slot, waiting no longer than ctx allows; every Acquire that returns nil needs one Release. A holder
// never takes a second slot, or holders could wait on each other forever.
func Acquire(ctx context.Context) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// Hold takes a slot for work that must finish whatever happens to its request, such as an erase.
func Hold() {
	slots <- struct{}{}
}

func Release() {
	<-slots
}
