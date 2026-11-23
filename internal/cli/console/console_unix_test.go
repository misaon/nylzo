//go:build !windows

package console

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

const withoutTerminalChild = "NYLZO_TEST_CONSOLE_WITHOUT_TERMINAL"

func TestReadSecretWithoutAControllingTerminalRefuses(t *testing.T) {
	if os.Getenv(withoutTerminalChild) != "" {
		_, err := NewSystem(os.Stdin, os.Stdout).ReadSecret("heslo: ")
		if !errors.Is(err, ErrNoTerminal) {
			os.Exit(1)
		}
		os.Exit(0)
	}
	t.Parallel()
	child := exec.CommandContext(
		t.Context(),
		os.Args[0],
		"-test.run=^TestReadSecretWithoutAControllingTerminalRefuses$",
	)
	child.Env = append(os.Environ(), withoutTerminalChild+"=1")
	if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
		child.Env = append(child.Env, "GOCOVERDIR="+directory.Value.String())
	}
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("a session without a terminal: %v\n%s", err, output)
	}
}
