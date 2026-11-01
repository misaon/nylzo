//go:build !linux

package operator

import (
	"errors"
	"runtime"
)

func hardenProcess() error {
	return nil
}

func lockMemory() error {
	return errors.New("nylzo locks its memory against swap only on Linux, not on " + runtime.GOOS)
}
