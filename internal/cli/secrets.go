package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
)

const (
	maxSecretInput   = 4096
	manageTokenLines = 2
	manageTokenPath  = "/m/"
)

var (
	errEmpty            = errors.New("the input is empty")
	errPassphraseSource = errors.New("give the passphrase in one way only: a file, the terminal or a generated file")
	errMismatch         = errors.New("the two passphrases differ")
	errManageToken      = errors.New(
		"a manage token has two lines: the message address without the part after # and the token",
	)
	errManageTokenSource = errors.New(
		"pass the manage token with --manage-token-file, " + command.EnvironmentManageToken + " or on stdin",
	)
)

func noteSource(environment *command.Environment, what, variable string) {
	fmt.Fprintf(environment.Stderr, "%s: using the %s named by %s\n", command.Program, what, variable)
}

func readLink(environment *command.Environment, linkFile string) (string, error) {
	var raw []byte
	var err error
	switch {
	case linkFile != "":
		raw, err = command.ReadFileLimited(linkFile, maxSecretInput)
	case environment.Getenv(command.EnvironmentLink) != "":
		noteSource(environment, "link", command.EnvironmentLink)
		raw = []byte(environment.Getenv(command.EnvironmentLink))
	case environment.Terminal.StdinIsTerminal():
		raw, err = environment.Terminal.ReadSecret("Link (hidden): ")
	default:
		raw, err = command.ReadLimited(environment.Stdin, maxSecretInput)
	}
	if err != nil {
		return "", err
	}
	link := strings.TrimSpace(string(raw))
	clear(raw)
	if link == "" {
		return "", errEmpty
	}
	return link, nil
}

type passphraseSource struct {
	file string
	ask  bool
}

func (p passphraseSource) given(environment *command.Environment) bool {
	return p.file != "" || environment.Getenv(command.EnvironmentPassphrase) != "" || p.ask
}

func readPassphrase(environment *command.Environment, source passphraseSource, confirm bool) (string, error) {
	path := source.file
	if path == "" && environment.Getenv(command.EnvironmentPassphrase) != "" {
		path = environment.Getenv(command.EnvironmentPassphrase)
		noteSource(environment, "passphrase file", command.EnvironmentPassphrase)
	}
	if path != "" && source.ask {
		return "", errPassphraseSource
	}
	if path != "" {
		return passphraseFromFile(path)
	}
	first, err := environment.Terminal.ReadSecret("Passphrase (hidden): ")
	if err != nil {
		return "", err
	}
	defer clear(first)
	if len(first) == 0 {
		return "", errEmpty
	}
	if confirm {
		second, secondErr := environment.Terminal.ReadSecret("Passphrase again: ")
		if secondErr != nil {
			return "", secondErr
		}
		defer clear(second)
		if !bytes.Equal(first, second) {
			return "", errMismatch
		}
	}
	return string(first), nil
}

func passphraseFromFile(path string) (string, error) {
	raw, err := command.ReadFileLimited(path, maxSecretInput)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	line, _, _ := bytes.Cut(raw, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	if len(line) == 0 {
		return "", errEmpty
	}
	return string(line), nil
}

type manageToken struct {
	origin string
	id     core.ID
	seed   [core.SeedLen]byte
}

func (m *manageToken) wipe() {
	clear(m.seed[:])
}

func formatManageToken(origin string, id core.ID, seed *[core.SeedLen]byte) string {
	return fmt.Sprintf("%s%s%s\n%s\n", origin, manageTokenPath, id, core.Encode(seed[:]))
}

func readManageToken(environment *command.Environment, file string) (manageToken, error) {
	path := file
	if path == "" && environment.Getenv(command.EnvironmentManageToken) != "" {
		path = environment.Getenv(command.EnvironmentManageToken)
		noteSource(environment, "manage token file", command.EnvironmentManageToken)
	}
	var raw []byte
	var err error
	switch {
	case path != "":
		raw, err = command.ReadFileLimited(path, maxSecretInput)
	case environment.Terminal.StdinIsTerminal():
		return manageToken{}, errManageTokenSource
	default:
		raw, err = command.ReadLimited(environment.Stdin, maxSecretInput)
	}
	if err != nil {
		return manageToken{}, err
	}
	defer clear(raw)
	return parseManageToken(string(raw))
}

func parseManageToken(raw string) (manageToken, error) {
	var lines []string
	for line := range strings.Lines(raw) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != manageTokenLines || strings.Contains(lines[0], "#") {
		return manageToken{}, errManageToken
	}
	address, err := url.Parse(lines[0])
	if err != nil || address.RawQuery != "" || address.User != nil {
		return manageToken{}, errManageToken
	}
	origin, err := client.Origin(address.Scheme + "://" + address.Host)
	if err != nil {
		return manageToken{}, err
	}
	encodedID, ok := strings.CutPrefix(address.EscapedPath(), manageTokenPath)
	id, err := core.ParseID(encodedID)
	if !ok || err != nil {
		return manageToken{}, errManageToken
	}
	seed, err := core.Decode32(lines[1])
	if err != nil {
		return manageToken{}, errManageToken
	}
	token := manageToken{origin: origin, id: id, seed: *seed}
	clear(seed[:])
	return token, nil
}

func linkOrigin(link string) (string, error) {
	head, _, _ := strings.Cut(link, "#")
	address, err := url.Parse(head)
	if err != nil {
		return "", err
	}
	return client.Origin(address.Scheme + "://" + address.Host)
}
