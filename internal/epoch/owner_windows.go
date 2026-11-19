package epoch

import "io/fs"

// The server refuses Windows; every entry counts as foreign and exposed, so nothing vouches for a Windows file.
func ownedByProcess(fs.FileInfo) bool {
	return false
}

func shared(fs.FileInfo) bool {
	return true
}
