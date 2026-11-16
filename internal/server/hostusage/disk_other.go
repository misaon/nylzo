//go:build !(linux || darwin)

package hostusage

import (
	"errors"
	"fmt"
)

var errDiskUnsupported = fmt.Errorf("hostusage: disk usage is not measured on this system: %w", errors.ErrUnsupported)

func DiskOf(string) (Disk, error) {
	return Disk{}, errDiskUnsupported
}
