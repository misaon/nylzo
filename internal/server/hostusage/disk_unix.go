//go:build linux || darwin

package hostusage

import "syscall"

func DiskOf(directory string) (Disk, error) {
	var fileSystem syscall.Statfs_t
	if err := syscall.Statfs(directory, &fileSystem); err != nil {
		return Disk{}, err
	}
	blockSize := uint64(max(fileSystem.Bsize, 0))
	return Disk{FreeBytes: fileSystem.Bavail * blockSize, TotalBytes: fileSystem.Blocks * blockSize}, nil
}
