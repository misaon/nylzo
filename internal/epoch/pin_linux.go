package epoch

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// pinFile maps the seed file and locks the mapping, so the kernel never moves its pages to swap while it lasts.
func pinFile(path string) ([]byte, error) {
	mapping, err := mapSeed(path)
	if err != nil {
		return nil, fmt.Errorf("epoch: lock the seed file in memory: %w", err)
	}
	return mapping, nil
}

func mapSeed(path string) ([]byte, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(descriptor), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if seedErr := checkSeedInfo(info); seedErr != nil {
		return nil, seedErr
	}
	if info.Size() != FileLen {
		return nil, fmt.Errorf("%w: %d bytes", ErrCorrupt, info.Size())
	}
	mapping, err := unix.Mmap(descriptor, 0, FileLen, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	if err = unix.Mlock(mapping); err != nil {
		_ = unix.Munmap(mapping)
		return nil, err
	}
	return mapping, nil
}

func unpinFile(mapping []byte) {
	if mapping != nil {
		_ = unix.Munmap(mapping)
	}
}
