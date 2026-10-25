//go:build !windows

package console

import "os"

func openTerminal() (terminalFiles, error) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	return terminalFiles{input: terminal, output: terminal}, err
}
