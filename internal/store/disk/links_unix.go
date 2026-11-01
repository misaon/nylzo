//go:build !windows

package disk

import (
	"os"
	"syscall"
)

func linkCount(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return widen(stat.Nlink)
	}
	return 1
}

// widen takes Nlink in each platform's width: uint64 on linux/amd64, uint32 on linux/arm64, uint16 on darwin.
func widen[Count uint16 | uint32 | uint64](count Count) uint64 {
	return uint64(count)
}
