//go:build !linux

package operator

import (
	"errors"
	"runtime"
)

var errMemoryLockLinuxOnly = errors.New("nylzo locks its memory against swap only on Linux, not on " + runtime.GOOS)

func hardenProcess() error {
	return nil
}

func lockMemory() error {
	return errMemoryLockLinuxOnly
}
