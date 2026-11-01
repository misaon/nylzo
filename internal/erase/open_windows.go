package erase

import "os"

func openForOverwrite(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY, 0)
}
