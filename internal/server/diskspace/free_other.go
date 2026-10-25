//go:build !(linux || darwin)

package diskspace

import "errors"

const Measurable = false

func Free(string) (uint64, error) {
	return 0, errors.New("diskspace: free space is not measured on this system")
}
