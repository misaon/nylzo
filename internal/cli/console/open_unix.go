//go:build !windows

package console

import "os"

func openTerminal() (terminalFiles, error) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0) //nolint:forbidigo // opens the terminal device
	return terminalFiles{input: terminal, output: terminal}, err
}
