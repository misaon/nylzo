//go:build !linux

package disk

import "os"

func openBlob(p string) (*os.File, error) {
	return os.Open(p)
}
