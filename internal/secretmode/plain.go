//go:build !goexperiment.runtimesecret

// Package secretmode runs request cryptography in runtime/secret's secret mode where the build enables it.
package secretmode

// Do runs f; builds without GOEXPERIMENT=runtimesecret have no secret mode, and the release build sets it.
func Do(f func()) {
	f()
}

func Supported() bool {
	return false
}
