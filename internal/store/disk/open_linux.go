package disk

import (
	"errors"
	"os"
	"syscall"
)

func openBlob(p string) (*os.File, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOATIME, 0)
	if errors.Is(err, syscall.EPERM) {
		return os.Open(p)
	}
	return f, err
}
