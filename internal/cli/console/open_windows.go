//go:build windows

package console

import "os"

func openTerminal() (terminalFiles, error) {
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return terminalFiles{}, err
	}
	output, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		input.Close()
		return terminalFiles{}, err
	}
	return terminalFiles{input: input, output: output}, nil
}
