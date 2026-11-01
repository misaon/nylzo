//go:build !windows

package erase

import (
	"os"
	"syscall"
)

// openForOverwrite never follows a link and never blocks on a FIFO, so it only writes into the file it names.
func openForOverwrite(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
}
