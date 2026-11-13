//go:build !linux

package erase

import "runtime"

// RequireInPlace refuses every path outside Linux, where no supported file system is known to overwrite in place.
func RequireInPlace(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	return checkInPlace(paths[0], mount{fileSystem: "a " + runtime.GOOS + " file system"})
}

// OnTmpfs reports false outside Linux, where the server knows no file system that keeps files only in memory.
func OnTmpfs(string) (bool, error) {
	return false, nil
}
