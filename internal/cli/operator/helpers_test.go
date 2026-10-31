package operator

import (
	"bytes"
	"context"
	"crypto/hpke"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
)

var testPepper = strings.Repeat("0123456789abcdef", 4)

var testReportKey = func() hpke.PrivateKey {
	key, err := abuse.NewKeyPair()
	if err != nil {
		panic(err)
	}
	return key
}()

var testReportKeyText = core.Encode(testReportKey.PublicKey().Bytes())

type noTerminal struct{}

func (noTerminal) StdinIsTerminal() bool { return false }

func (noTerminal) StdoutIsTerminal() bool { return false }

func (noTerminal) ReadSecret(string) ([]byte, error) { return nil, console.ErrNoTerminal }

type commandResult struct {
	code   int
	stdout string
	stderr string
}

func getenvFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func run(
	t *testing.T,
	stdin string,
	variables map[string]string,
	terminal console.Terminal,
	operatorCommand func(context.Context, *command.Environment, []string) int,
	arguments ...string,
) commandResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if terminal == nil {
		terminal = noTerminal{}
	}
	code := operatorCommand(t.Context(), &command.Environment{
		Stdin:    strings.NewReader(stdin),
		Stdout:   &stdout,
		Stderr:   &stderr,
		Getenv:   getenvFrom(variables),
		Terminal: terminal,
		Version:  "test",
	}, arguments)
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}
