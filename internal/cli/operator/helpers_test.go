package operator

import (
	"bytes"
	"context"
	"crypto/hpke"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
)

var testPepper = strings.Repeat("0123456789abcdef", 4)

// testDevelopment keeps the storage check on Linux, where CI runs, and allows swap, since tests do not lock memory.
var testDevelopment = config.Development{AllowSwap: true, AllowUnerasableStorage: runtime.GOOS != "linux"}

func pepperFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pepper")
	if err := os.WriteFile(path, []byte(testPepper), 0o400); err != nil {
		t.Fatal(err)
	}
	return path
}

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
	variables = maps.Clone(variables)
	if variables == nil {
		variables = map[string]string{}
	}
	for name, allowed := range map[string]bool{
		"NYLZO_ALLOW_SWAP":               testDevelopment.AllowSwap,
		"NYLZO_ALLOW_UNERASABLE_STORAGE": testDevelopment.AllowUnerasableStorage,
	} {
		if _, set := variables[name]; !set && allowed {
			variables[name] = "1"
		}
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
