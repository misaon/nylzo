//go:build !windows

package epoch

import (
	"io/fs"
	"os"
	"syscall"
)

func ownedByProcess(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

func shared(info fs.FileInfo) bool {
	return info.Mode().Perm()&0o077 != 0
}
