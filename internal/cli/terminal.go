package cli

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"
)

var ErrNoTerminal = errors.New("no terminal to ask on")

type Terminal interface {
	StdinIsTerminal() bool
	ReadSecret(prompt string) ([]byte, error)
}

type systemTerminal struct {
	stdin *os.File
}

func SystemTerminal(stdin *os.File) Terminal {
	return systemTerminal{stdin: stdin}
}

func (t systemTerminal) StdinIsTerminal() bool {
	return term.IsTerminal(int(t.stdin.Fd()))
}

func (t systemTerminal) ReadSecret(prompt string) ([]byte, error) {
	in, out, err := openTerminal()
	if err != nil {
		return nil, ErrNoTerminal
	}
	defer in.Close()
	if out != in {
		defer out.Close()
	}
	fmt.Fprint(out, prompt)
	secret, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	return secret, err
}
