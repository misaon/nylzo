//go:build goexperiment.runtimesecret

// Package secretmode runs request cryptography in runtime/secret's secret mode where the build enables it.
package secretmode

import "runtime/secret"

const Enabled = true

// Do erases the registers and stack that f used when it returns, and its heap allocations once they are unreachable.
func Do(f func()) {
	secret.Do(f)
}
