//go:build !windows

package epoch

import (
	"os"
	"syscall"
)

func lock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

// lockShared lets a read wait for a rewrite in progress, so it never sees a slot half written.
func lockShared(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_SH)
}

func unlock(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
