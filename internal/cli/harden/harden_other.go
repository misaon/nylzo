//go:build !linux && !darwin && !windows

package harden

import (
	"errors"
	"runtime"
)

var errUnsupported = errors.New(
	"nylzo keeps its secrets out of core files and away from debuggers only on Linux, macOS and Windows, not on " +
		runtime.GOOS,
)

func disableDumps() error {
	return errUnsupported
}

func LockMemory() error {
	return errUnsupported
}

func keepFromSwap() string {
	return ""
}
