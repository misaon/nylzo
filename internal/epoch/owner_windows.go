package epoch

import "io/fs"

// Windows runs only development servers and has no POSIX owner or mode to check.
func ownedByProcess(fs.FileInfo) bool {
	return true
}

func shared(fs.FileInfo) bool {
	return false
}
