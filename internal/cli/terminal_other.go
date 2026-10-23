//go:build !windows

package cli

import "os"

func openTerminal() (*os.File, *os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	return tty, tty, err
}
