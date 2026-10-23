//go:build !(linux || darwin)

package server

import "errors"

const freeSpaceSupported = false

func freeSpace(string) (uint64, error) {
	return 0, errors.New("server: free space is not measured on this system")
}
