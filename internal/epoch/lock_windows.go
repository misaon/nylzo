package epoch

import (
	"errors"
	"os"
)

// The server refuses Windows (D54); refuse rather than use a seed file without a lock.
func lock(*os.File) error {
	return errors.ErrUnsupported
}

func lockShared(*os.File) error {
	return errors.ErrUnsupported
}

func unlock(*os.File) {}
