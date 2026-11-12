//go:build windows

package stopsignal

import "os"

// Exit exits with 128 plus the number of received, since Windows cannot end a process by a signal.
func Exit(received os.Signal) {
	os.Exit(exitCode(received))
}
