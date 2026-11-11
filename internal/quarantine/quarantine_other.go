//go:build !darwin && !windows

package quarantine

import "os"

// Mark does nothing where the system has no download mark, as on Linux.
func Mark(*os.File) error {
	return nil
}
