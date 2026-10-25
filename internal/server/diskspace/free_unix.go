//go:build linux || darwin

package diskspace

import "syscall"

const Measurable = true

func Free(directory string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(directory, &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 {
		return 0, nil
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
