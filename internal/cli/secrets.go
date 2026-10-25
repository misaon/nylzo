package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"strings"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
)

const (
	maxSecretInput   = 4096
	envLink          = "NYLZO_LINK"
	envPassphrase    = "NYLZO_PASSPHRASE_FILE"   //nolint:gosec // the name of a variable that names a file, not a credential
	envManageToken   = "NYLZO_MANAGE_TOKEN_FILE" //nolint:gosec // the name of a variable that names a file, not a credential
	envServer        = "NYLZO_SERVER"
	manageTokenLines = 2
)

var (
	errEmpty            = errors.New("the input is empty")
	errTooLong          = errors.New("the input is longer than nylzo accepts")
	errPassphraseSource = errors.New("give the passphrase either in a file or from the terminal, not both")
	errMismatch         = errors.New("the two passphrases differ")
	errManageToken      = errors.New(
		"a manage token has two lines: the message address without the part after # and the token",
	)
)

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLong
	}
	return data, nil
}

func readFileLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, unreadable(err)
	}
	defer file.Close()
	data, err := readLimited(file, limit)
	if err != nil && !errors.Is(err, errTooLong) {
		return nil, unreadable(err)
	}
	return data, err
}

// unreadable drops the path from a file error, so no message repeats a flag value.
func unreadable(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("the file does not exist")
	case errors.Is(err, fs.ErrPermission):
		return errors.New("the file is not readable")
	default:
		return errors.New("the file cannot be read")
	}
}

func noteSource(environment *Environment, what, variable string) {
	fmt.Fprintf(environment.Stderr, "%s: using the %s named by %s\n", program, what, variable)
}

func readLink(environment *Environment, linkFile string) (string, error) {
	var raw []byte
	var err error
	switch {
	case linkFile != "":
		raw, err = readFileLimited(linkFile, maxSecretInput)
	case environment.Getenv(envLink) != "":
		noteSource(environment, "link", envLink)
		raw = []byte(environment.Getenv(envLink))
	case environment.Terminal.StdinIsTerminal():
		raw, err = environment.Terminal.ReadSecret("Link (hidden): ")
	default:
		raw, err = readLimited(environment.Stdin, maxSecretInput)
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

func (p passphraseSource) given(environment *Environment) bool {
	return p.file != "" || environment.Getenv(envPassphrase) != "" || p.ask
}

func readPassphrase(environment *Environment, source passphraseSource, confirm bool) (string, error) {
	path := source.file
	if path == "" && environment.Getenv(envPassphrase) != "" {
		path = environment.Getenv(envPassphrase)
		noteSource(environment, "passphrase file", envPassphrase)
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
	raw, err := readFileLimited(path, maxSecretInput)
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
	return fmt.Sprintf("%s/m/%s\n%s\n", origin, id, core.Encode(seed[:]))
}

func readManageToken(environment *Environment, file string) (manageToken, error) {
	path := file
	if path == "" && environment.Getenv(envManageToken) != "" {
		path = environment.Getenv(envManageToken)
		noteSource(environment, "manage token file", envManageToken)
	}
	var raw []byte
	var err error
	switch {
	case path != "":
		raw, err = readFileLimited(path, maxSecretInput)
	case environment.Terminal.StdinIsTerminal():
		return manageToken{}, errors.New(
			"pass the manage token with --manage-token-file, " + envManageToken + " or on stdin",
		)
	default:
		raw, err = readLimited(environment.Stdin, maxSecretInput)
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
	id, err := core.ParsePath(address.EscapedPath())
	if err != nil {
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
