package disk

import (
	"errors"
	"os"
	"syscall"
)

func openBlob(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOATIME, 0)
	if errors.Is(err, syscall.EPERM) {
		return os.Open(path)
	}
	return file, err
}
