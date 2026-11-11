//go:build !windows && !darwin && !linux

package privatefile

import (
	"errors"
	"os"
)

func createNew(string) (*os.File, error) {
	return nil, errors.ErrUnsupported
}

func mkdirIn(*os.Root, string, string) error {
	return errors.ErrUnsupported
}

func enforcedLocally(*os.File) bool {
	return false
}

func withoutACL(*os.File) bool {
	return false
}
