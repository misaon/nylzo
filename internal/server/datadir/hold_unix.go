//go:build !windows

package datadir

import (
	"errors"
	"os"
	"syscall"
)

// Hold locks the data directory for as long as this process serves it, so a second serve refuses to start: a
// process accepts only the request challenges it issued itself (spec section 5.5).
func Hold(directory string) (func(), error) {
	if err := os.MkdirAll(directory, privateDirectoryMode); err != nil {
		return nil, err
	}
	held, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		held.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrServed
		}
		return nil, err
	}
	return func() { held.Close() }, nil
}
