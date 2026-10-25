package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
)

const (
	textFileName     = "message.txt"
	blobRetries      = 3
	outputDirPrefix  = "nylzo-"
	attachmentPrefix = "attachment-"
	releasesURL      = "https://github.com/misaon/nylzo/releases"
)

var (
	errWriteOutput = errors.New(
		"the files could not be written; check the free space and permissions of the output directory",
	)
	errTampered = errors.New(
		"the message could not be decrypted: it was changed, or the link or passphrase is wrong",
	)
	errPolicyChanged = errors.New(
		"the server sent the message with a different policy than its metadata, so nylzo refused it",
	)
)

type readOptions struct {
	linkFile   string
	passphrase passphraseSource
	out        string
	stdout     bool
}

type openLink struct {
	origin string
	id     core.ID
	kdf    core.KDF
	secret [core.SecretLen]byte
	keys   core.LinkKeys
}

func (l *openLink) wipe() {
	clear(l.secret[:])
	clear(l.keys.R[:])
	clear(l.keys.SaltPW[:])
}

func parseOpenLink(raw string) (openLink, error) {
	parsed, err := core.ParseLink(raw)
	switch {
	case errors.Is(err, core.ErrLinkVersion):
		return openLink{}, errors.New("the link was made by a newer version of nylzo; update nylzo from " + releasesURL)
	case errors.Is(err, core.ErrLinkRetiredKDF):
		return openLink{}, errors.New(
			"the link uses a passphrase profile that nylzo no longer supports; ask the sender for a new link",
		)
	case err != nil:
		return openLink{}, errors.New("the link is incomplete or damaged")
	default:
	}
	origin, err := linkOrigin(raw)
	if err != nil {
		clear(parsed.Secret[:])
		return openLink{}, err
	}
	link := openLink{origin: origin, id: parsed.ID, kdf: parsed.KDF, secret: parsed.Secret}
	clear(parsed.Secret[:])
	if link.keys, err = core.DeriveLink(link.id, &link.secret); err != nil {
		link.wipe()
		return openLink{}, err
	}
	return link, nil
}

func linkFromInput(environment *Environment, linkFile string) (openLink, error) {
	raw, err := readLink(environment, linkFile)
	if err != nil {
		return openLink{}, err
	}
	return parseOpenLink(raw)
}

func readFlags(environment *Environment) (*flag.FlagSet, *readOptions) {
	options := &readOptions{}
	flags := newFlags(environment, "read")
	flags.StringVar(&options.linkFile, "link-file", "", "read the link from this file (or "+envLink+", or stdin)")
	flags.StringVar(
		&options.passphrase.file,
		"passphrase-file",
		"",
		"read the passphrase from the first line of this file (or "+envPassphrase+")",
	)
	flags.BoolVar(
		&options.passphrase.ask,
		"ask-passphrase",
		false,
		"ask for the passphrase on the terminal without echo",
	)
	flags.StringVar(
		&options.out,
		"out",
		"",
		"create the new private directory for the files inside this directory (default: the system temporary directory)",
	)
	flags.BoolVar(&options.stdout, "stdout", false, "print the text to stdout instead of writing it to a file")
	return flags, options
}

// runRead creates the output directory before the claim, so a write problem never spends an open.
func runRead(ctx context.Context, environment *Environment, arguments []string) int {
	flags, options := readFlags(environment)
	if code, done := flagsDone(parseFlags(flags, environment, arguments)); done {
		return code
	}
	link, err := linkFromInput(environment, options.linkFile)
	if err != nil {
		return failf(environment, "link: %s", explain(err))
	}
	defer link.wipe()
	api, err := client.New(link.origin)
	if err != nil {
		return failf(environment, "%s", explain(err))
	}
	base := options.out
	if base == "" {
		base = environment.tempDir()
	}
	directory, err := outputDir(base, options.out != "")
	if err != nil {
		return failf(environment, "%s", err)
	}
	defer removeIfEmpty(directory)
	opened, err := readMessage(ctx, environment, api, &link, options.passphrase)
	if err != nil {
		return failf(environment, "%s", explain(err))
	}
	defer clear(opened.payload.File)
	paths, err := saveMessage(environment, directory, base, options.stdout, opened.payload)
	if err != nil {
		return failf(environment, "the message was opened but could not be saved: %s", err)
	}
	if err = api.Ack(ctx, link.id, &link.keys.R, opened.lease); err != nil && !client.IsKind(err, "gone") {
		fmt.Fprintf(
			environment.Stderr,
			"%s: warning: the server did not confirm the read; it frees the message when the lease ends\n",
			program,
		)
	}
	for _, path := range paths {
		fmt.Fprintln(environment.Stdout, path)
	}
	return exitOK
}

type openedMessage struct {
	payload core.Payload
	lease   string
}

func readMessage(
	ctx context.Context,
	environment *Environment,
	api *client.Client,
	link *openLink,
	source passphraseSource,
) (openedMessage, error) {
	metadata, err := api.Metadata(ctx, link.id, &link.keys.R)
	if err != nil {
		return openedMessage{}, err
	}
	if metadata.State != "available" {
		return openedMessage{}, &client.APIError{Status: 410, Kind: "gone", State: metadata.State}
	}
	if err = core.AssertProfile(link.kdf, metadata.KDF); err != nil {
		return openedMessage{}, errors.New("the link does not match the message on the server")
	}
	passphraseKey, err := passphraseKey(environment, link, source)
	if err != nil {
		return openedMessage{}, err
	}
	keys, err := core.DeriveContent(link.id, &link.secret, passphraseKey)
	clear(passphraseKey)
	if err != nil {
		return openedMessage{}, err
	}
	defer clear(keys.K[:])
	defer clear(keys.V[:])
	var verifier *[core.TokenLen]byte
	if link.kdf != core.KDFNone {
		verifier = &keys.V
	}
	envelope, lease, err := fetchEnvelope(ctx, api, link, verifier, metadata.Size)
	if err != nil {
		return openedMessage{}, err
	}
	if envelope.Flags != 0 || envelope.KDF != link.kdf || int(envelope.Policy.OpensMax) != metadata.OpensMax ||
		int64(envelope.Policy.TTLSeconds) != metadata.TTLSeconds {
		return openedMessage{}, errPolicyChanged
	}
	_, payload, err := core.Open(&keys.K, link.id, envelope.Policy, link.kdf, envelope.Object)
	clear(envelope.Object)
	if err != nil {
		return openedMessage{}, errTampered
	}
	return openedMessage{payload: payload, lease: lease}, nil
}

func passphraseKey(environment *Environment, link *openLink, source passphraseSource) ([]byte, error) {
	if link.kdf == core.KDFNone {
		return nil, nil
	}
	if !source.given(environment) {
		source.ask = true
	}
	passphrase, err := readPassphrase(environment, source, false)
	if errors.Is(err, console.ErrNoTerminal) {
		return nil, errors.New(
			"this message needs a passphrase: use --passphrase-file, " + envPassphrase + " or --ask-passphrase",
		)
	}
	if err != nil {
		return nil, err
	}
	return core.DerivePassphraseKey(link.kdf, passphrase, &link.keys.SaltPW)
}

// fetchEnvelope resumes a broken claim through the lease it already received, so it never spends a second open.
func fetchEnvelope(
	ctx context.Context,
	api *client.Client,
	link *openLink,
	verifier *[core.TokenLen]byte,
	size int64,
) (client.Envelope, string, error) {
	claimed, err := api.Claim(ctx, link.id, &link.keys.R, verifier, size, nil)
	if err == nil {
		return claimed.Envelope, claimed.Lease, nil
	}
	if claimed.Lease == "" {
		return client.Envelope{}, "", err
	}
	for range blobRetries {
		envelope, blobErr := api.Blob(ctx, link.id, &link.keys.R, claimed.Lease, size)
		if blobErr == nil {
			return envelope, claimed.Lease, nil
		}
		err = blobErr
		if client.IsKind(blobErr, "gone") || client.IsKind(blobErr, "rate_limited") || ctx.Err() != nil {
			break
		}
	}
	return client.Envelope{}, "", err
}

func terminalControl(character rune) bool {
	return character != '\n' && character != '\t' && (unicode.IsControl(character) || unicode.Is(unicode.Cf, character))
}

type outputFile struct {
	name string
	data []byte
}

func saveMessage(
	environment *Environment,
	directory, base string,
	toStdout bool,
	payload core.Payload,
) ([]string, error) {
	var files []outputFile
	if text := payload.Meta.Text; text != nil {
		if toStdout && environment.Terminal.StdoutIsTerminal() && strings.ContainsFunc(*text, terminalControl) {
			fmt.Fprintf(
				environment.Stderr,
				"%s: the text holds terminal control characters, so it goes to a file instead of the terminal\n",
				program,
			)
			toStdout = false
		}
		if toStdout {
			printText(environment, *text)
		} else {
			files = append(files, outputFile{textFileName, []byte(*text)})
		}
	}
	if payload.Meta.File != nil {
		name := attachmentName(payload.Meta.File.Name)
		if sameFileName(name, textFileName) {
			name = attachmentPrefix + name
		}
		files = append(files, outputFile{name, payload.File})
	}
	paths, err := writeWithFallback(environment, directory, base, files)
	if err != nil {
		return nil, err
	}
	if toStdout {
		for _, path := range paths {
			fmt.Fprintf(environment.Stderr, "%s: the attachment is in %s\n", program, path)
		}
		return nil, nil
	}
	return paths, nil
}

func printText(environment *Environment, text string) {
	fmt.Fprint(environment.Stdout, text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		fmt.Fprintln(environment.Stdout)
	}
}

// writeWithFallback tries a second new directory next to the first, so a failed write does not lose the message.
func writeWithFallback(environment *Environment, directory, base string, files []outputFile) ([]string, error) {
	paths, err := writeAll(directory, files)
	if err == nil {
		return paths, nil
	}
	fallback, err := os.MkdirTemp(base, outputDirPrefix)
	if err != nil {
		return nil, errWriteOutput
	}
	fmt.Fprintf(
		environment.Stderr,
		"%s: warning: writing the files failed, trying a second new directory next to the first\n",
		program,
	)
	if paths, err = writeAll(fallback, files); err != nil {
		removeIfEmpty(fallback)
		return nil, errWriteOutput
	}
	return paths, nil
}

func writeAll(directory string, files []outputFile) ([]string, error) {
	var paths []string
	for _, file := range files {
		path, err := writePrivate(directory, file.name, file.data)
		if errors.Is(err, fs.ErrExist) && !strings.HasPrefix(file.name, attachmentPrefix) {
			path, err = writePrivate(directory, attachmentPrefix+file.name, file.data)
		}
		if err != nil {
			for _, written := range paths {
				os.Remove(written)
			}
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func outputDir(base string, given bool) (string, error) {
	if given {
		if info, err := os.Stat(base); err != nil || !info.IsDir() {
			return "", errors.New("--out must name an existing directory")
		}
	}
	directory, err := os.MkdirTemp(base, outputDirPrefix)
	if err == nil {
		return directory, nil
	}
	if given {
		return "", errors.New("--out: cannot create a private directory there")
	}
	return "", errors.New("cannot create a private directory in the temporary directory")
}

func removeIfEmpty(directory string) {
	if entries, err := os.ReadDir(directory); err == nil && len(entries) == 0 {
		os.Remove(directory)
	}
}

// writePrivate creates a new file only inside dir and never replaces one.
func writePrivate(directory, name string, data []byte) (string, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return "", err
	}
	_, err = file.Write(data)
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		_ = root.Remove(name)
		return "", err
	}
	return filepath.Join(directory, name), nil
}
