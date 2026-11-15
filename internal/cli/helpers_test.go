package cli

import "github.com/misaon/nylzo/internal/cli/console"

type noTerminal struct{}

func (noTerminal) StdinIsTerminal() bool { return false }

func (noTerminal) StdoutIsTerminal() bool { return false }

func (noTerminal) ReadSecret(string) ([]byte, error) { return nil, console.ErrNoTerminal }

func getenvFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}
