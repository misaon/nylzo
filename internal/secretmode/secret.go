//go:build goexperiment.runtimesecret

// Package secretmode runs request cryptography in runtime/secret's secret mode where the build enables it.
package secretmode

import (
	"runtime"
	"runtime/secret"
)

// Do erases the registers and stack that f used when it returns, and its heap allocations once they are unreachable.
func Do(f func()) {
	secret.Do(f)
}

// Supported reports whether Do erases anything; elsewhere runtime/secret calls f directly.
func Supported() bool {
	return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
}
