//go:build !linux

package erase

import "runtime"

// RequireInPlace refuses every directory outside Linux, where no supported file system is known to overwrite in place.
func RequireInPlace(directory string) error {
	return checkInPlace(directory, mount{fileSystem: "a " + runtime.GOOS + " file system"})
}
