package cli

import (
	"crypto/hpke"

	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/server/abuse"
)

type noTerminal struct{}

func (noTerminal) StdinIsTerminal() bool { return false }

func (noTerminal) StdoutIsTerminal() bool { return false }

func (noTerminal) ReadSecret(string) ([]byte, error) { return nil, console.ErrNoTerminal }

var testReportKey = func() hpke.PrivateKey {
	key, err := abuse.NewKeyPair()
	if err != nil {
		panic(err)
	}
	return key
}()

func getenvFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}
