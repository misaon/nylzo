//go:build !windows && !darwin

package privatefile

import "os"

func createNew(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
}

func mkdirNew(directory, prefix string) (string, error) {
	return os.MkdirTemp(directory, prefix+"*")
}

// withoutACL holds for Linux, where the group bits of a 0600 or 0700 mode set the ACL mask to no access.
func withoutACL(*os.File) bool {
	return true
}
