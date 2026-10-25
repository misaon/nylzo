//go:build !linux

package disk

import "os"

func openBlob(path string) (*os.File, error) {
	return os.Open(path)
}
