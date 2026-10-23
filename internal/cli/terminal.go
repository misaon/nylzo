package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

var ErrNoTerminal = errors.New("no terminal to ask on")

type Terminal interface {
	StdinIsTerminal() bool
	StdoutIsTerminal() bool
	ReadSecret(prompt string) ([]byte, error)
}

type systemTerminal struct {
	stdin  *os.File
	stdout *os.File
}

func SystemTerminal(stdin, stdout *os.File) Terminal {
	return systemTerminal{stdin: stdin, stdout: stdout}
}

func (t systemTerminal) StdinIsTerminal() bool {
	return term.IsTerminal(int(t.stdin.Fd()))
}

func (t systemTerminal) StdoutIsTerminal() bool {
	return term.IsTerminal(int(t.stdout.Fd()))
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
	fd := int(in.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return nil, ErrNoTerminal
	}
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupted)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-interrupted:
			_ = term.Restore(fd, state)
			fmt.Fprintln(out)
			os.Exit(exitInterrupted)
		case <-done:
		}
	}()
	fmt.Fprint(out, prompt)
	secret, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	return secret, err
}
