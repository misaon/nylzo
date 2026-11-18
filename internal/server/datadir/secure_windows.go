//go:build windows

package datadir

import "errors"

// The server refuses Windows; these refuse too rather than vouch for files whose access list they never read.
var errUnsupported = errors.New("datadir: the server's files are kept private only on Linux and macOS")

func Secure(Layout) (int, error) {
	return 0, errUnsupported
}

func Check(Layout) error {
	return errUnsupported
}
