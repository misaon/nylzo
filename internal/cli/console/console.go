package console

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

const exitInterrupted = 130

var ErrNoTerminal = errors.New("no terminal to ask on")

type Terminal interface {
	StdinIsTerminal() bool
	StdoutIsTerminal() bool
	ReadSecret(prompt string) ([]byte, error)
}

type terminalFiles struct {
	input  *os.File
	output *os.File
}

type System struct {
	stdin  *os.File
	stdout *os.File
}

func NewSystem(stdin, stdout *os.File) *System {
	return &System{stdin: stdin, stdout: stdout}
}

func (s *System) StdinIsTerminal() bool {
	return term.IsTerminal(int(s.stdin.Fd()))
}

func (s *System) StdoutIsTerminal() bool {
	return term.IsTerminal(int(s.stdout.Fd()))
}

// ReadSecret reads from the controlling terminal without echo and restores the echo when interrupted.
func (*System) ReadSecret(prompt string) ([]byte, error) {
	files, err := openTerminal()
	if err != nil {
		return nil, ErrNoTerminal
	}
	input, output := files.input, files.output
	defer input.Close()
	if output != input {
		defer output.Close()
	}
	descriptor := int(input.Fd())
	state, err := term.GetState(descriptor)
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
			_ = term.Restore(descriptor, state)
			fmt.Fprintln(output)
			os.Exit(exitInterrupted)
		case <-done:
		}
	}()
	fmt.Fprint(output, prompt)
	secret, err := term.ReadPassword(descriptor)
	fmt.Fprintln(output)
	return secret, err
}
