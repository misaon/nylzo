//go:build !windows

package stopsignal

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

const deliveryTime = time.Second

// Exit ends the process by received, as if nylzo had never caught it. Where a signal cannot end the process, as in
// the init process of a container, Go's runtime exits with 128 plus the signal number. Exit does the same at once
// when it cannot send the signal, and after a second when the signal has not ended the process.
func Exit(received os.Signal) {
	if number, ok := received.(syscall.Signal); ok {
		signal.Reset(received)
		if syscall.Kill(os.Getpid(), number) == nil {
			time.Sleep(deliveryTime)
		}
	}
	os.Exit(exitCode(received))
}
