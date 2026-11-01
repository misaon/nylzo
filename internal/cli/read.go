package cli

import (
	"context"
	"crypto/ecdh"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/share"
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
	errNoDestination = errors.New(
		"read needs --stdout, which prints the text and writes nothing to disk, " +
			"or --out DIR, which writes the text and any attachment into a new private directory inside DIR",
	)
	errAttachmentDiscarded = errors.New(
		"the message carries an attachment, which --stdout cannot deliver, so it was not saved; " +
			"a link does not show this before it is opened, so read links that may carry a file with --out DIR",
	)
)

type readOptions struct {
	linkFile   string
	passphrase passphraseSource
	out        string
	stdout     bool
}

type openLink struct {
	origin     string
	id         core.ID
	kdf        core.KDF
	secret     [core.SecretLen]byte
	objectHash [core.ObjectHashLen]byte
	keys       keys.LinkKeys
}

func (l *openLink) wipe() {
	clear(l.secret[:])
	clear(l.keys.R[:])
	clear(l.keys.PassphraseSalt[:])
}

func parseOpenLink(raw string) (openLink, error) {
	parsed, err := core.ParseLink(raw)
	switch {
	case errors.Is(err, core.ErrLinkVersion):
		return openLink{}, errors.New("the link was made by a newer version of nylzo; update nylzo from " + releasesURL)
	case errors.Is(err, core.ErrLinkReleased):
		return openLink{}, errors.New(
			"the link is from before this instance's security upgrade, which deleted every message it held; " +
				"ask the sender for a new link",
		)
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
	link := openLink{
		origin:     origin,
		id:         parsed.ID,
		kdf:        parsed.KDF,
		secret:     parsed.Secret,
		objectHash: parsed.ObjectHash,
	}
	parsed.Wipe()
	if link.keys, err = keys.DeriveLink(link.id, &link.secret); err != nil {
		link.wipe()
		return openLink{}, err
	}
	return link, nil
}

func linkFromInput(environment *command.Environment, linkFile string) (openLink, error) {
	raw, err := readLink(environment, linkFile)
	if err != nil {
		return openLink{}, err
	}
	return parseOpenLink(raw)
}

func readFlags(environment *command.Environment) (*flag.FlagSet, *readOptions) {
	options := &readOptions{}
	flags := command.NewFlags(environment, "read")
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
		"write the text and any attachment into a new private directory inside this existing directory",
	)
	flags.BoolVar(&options.stdout, "stdout", false, "print the text to stdout and write nothing to disk")
	return flags, options
}

// runRead creates the output directory before the claim, so a write problem never spends an open.
func runRead(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags, options := readFlags(environment)
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if !options.stdout && options.out == "" {
		fmt.Fprintln(environment.Stderr, command.Program+": "+errNoDestination.Error())
		return command.ExitUsage
	}
	link, err := linkFromInput(environment, options.linkFile)
	if err != nil {
		return command.Failf(environment, "link: %s", explain(err))
	}
	defer link.wipe()
	api, err := client.New(link.origin)
	if err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	directory := ""
	if options.out != "" {
		if directory, err = outputDir(options.out); err != nil {
			return command.Failf(environment, "%s", err)
		}
		defer removeIfEmpty(directory)
	}
	opened, err := readMessage(ctx, environment, api, &link, options.passphrase)
	if err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	defer clear(opened.payload.File)
	paths, err := saveMessage(environment, directory, options.out, options.stdout, opened.payload)
	if err != nil {
		return command.Failf(environment, "the message was opened but could not be saved: %s", err)
	}
	if err = api.Ack(ctx, link.id, &link.keys.R, opened.lease); err != nil && !client.IsKind(err, "gone") {
		fmt.Fprintf(
			environment.Stderr,
			"%s: warning: the server did not confirm the read; it frees the message when the lease ends\n",
			command.Program,
		)
	}
	for _, path := range paths {
		fmt.Fprintln(environment.Stdout, path)
	}
	if len(paths) > 0 {
		fmt.Fprintf(
			environment.Stderr,
			"%s: the files hold the plaintext; delete them when you no longer need them "+
				"(on SSDs and in backups a deletion may not erase them)\n", command.Program,
		)
	}
	return command.ExitOK
}

type openedMessage struct {
	payload envelope.Payload
	lease   string
}

func readMessage(
	ctx context.Context,
	environment *command.Environment,
	api *client.Client,
	link *openLink,
	source passphraseSource,
) (openedMessage, error) {
	metadata, err := api.Metadata(ctx, link.id, &link.keys.R)
	if err != nil {
		return openedMessage{}, err
	}
	if checkErr := checkMetadata(link, &metadata); checkErr != nil {
		return openedMessage{}, checkErr
	}
	passphraseKey, err := passphraseKey(environment, link, source)
	if err != nil {
		return openedMessage{}, err
	}
	defer clear(passphraseKey)
	verifier, err := keys.DeriveVerifier(link.id, &link.secret, passphraseKey)
	if err != nil {
		return openedMessage{}, err
	}
	defer clear(verifier[:])
	shareKey := share.NewKey()
	fetched, lease, err := fetchEnvelope(ctx, api, link, &verifier, shareKey, metadata.Size)
	if err != nil {
		return openedMessage{}, err
	}
	defer clear(fetched.Object)
	if !headerMatches(&fetched, link, &metadata) {
		return openedMessage{}, errPolicyChanged
	}
	contentKeys, err := contentKeysOf(link, passphraseKey, shareKey, fetched.Share)
	if err != nil {
		return openedMessage{}, errTampered
	}
	defer contentKeys.Wipe()
	expected := envelope.Expected{
		ID:         link.id,
		Policy:     fetched.Policy,
		KDF:        link.kdf,
		ObjectHash: &link.objectHash,
	}
	_, payload, err := envelope.Open(&contentKeys, &expected, fetched.Object)
	if err != nil {
		return openedMessage{}, errTampered
	}
	return openedMessage{payload: payload, lease: lease}, nil
}

// checkMetadata refuses before anything is spent: an ended message and a link of another profile.
func checkMetadata(link *openLink, metadata *client.Metadata) error {
	if metadata.State != "available" {
		return &client.APIError{Status: 410, Kind: "gone", State: metadata.State}
	}
	if err := core.AssertProfile(link.kdf, metadata.KDF); err != nil {
		return errors.New("the link does not match the message on the server")
	}
	return nil
}

func headerMatches(fetched *client.Envelope, link *openLink, metadata *client.Metadata) bool {
	return fetched.Flags == 0 && fetched.KDF == link.kdf && int(fetched.Policy.OpensMax) == metadata.OpensMax &&
		int64(fetched.Policy.TTLSeconds) == metadata.TTLSeconds
}

// contentKeysOf unwraps the share, which the server sends only with the object.
func contentKeysOf(
	link *openLink,
	passphraseKey []byte,
	shareKey *ecdh.PrivateKey,
	wrapped *[share.WrappedLen]byte,
) (keys.ContentKeys, error) {
	if wrapped == nil {
		return keys.ContentKeys{}, errTampered
	}
	expiryShare, err := share.Unwrap(shareKey, wrapped)
	if err != nil {
		return keys.ContentKeys{}, err
	}
	defer clear(expiryShare[:])
	return keys.DeriveContent(link.id, &link.secret, passphraseKey, &expiryShare)
}

func passphraseKey(environment *command.Environment, link *openLink, source passphraseSource) ([]byte, error) {
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
	return keys.DerivePassphraseKey(link.kdf, passphrase, &link.keys.PassphraseSalt)
}

// fetchEnvelope resumes a broken claim through the lease it already received, so it never spends a second open.
func fetchEnvelope(
	ctx context.Context,
	api *client.Client,
	link *openLink,
	verifier *[core.SeedLen]byte,
	shareKey *ecdh.PrivateKey,
	size int64,
) (client.Envelope, string, error) {
	claimed, err := api.Claim(ctx, link.id, &link.keys.R, verifier, shareKey.PublicKey(), size, nil)
	if err == nil {
		return claimed.Envelope, claimed.Lease, nil
	}
	if claimed.Lease == "" {
		return client.Envelope{}, "", err
	}
	for range blobRetries {
		fetched, blobErr := api.Blob(ctx, link.id, &link.keys.R, shareKey.PublicKey(), claimed.Lease, size)
		if blobErr == nil {
			return fetched, claimed.Lease, nil
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
	environment *command.Environment,
	directory, base string,
	toStdout bool,
	payload envelope.Payload,
) ([]string, error) {
	var files []outputFile
	if text := payload.Meta.Text; text != nil {
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
	if len(files) == 0 {
		return nil, nil
	}
	if directory == "" {
		return nil, errAttachmentDiscarded
	}
	defer wipeOutputFiles(files)
	paths, err := writeWithFallback(environment, directory, base, files)
	if err != nil {
		return nil, err
	}
	if toStdout {
		for _, path := range paths {
			fmt.Fprintf(environment.Stderr, "%s: the attachment is in %s\n", command.Program, path)
		}
		return nil, nil
	}
	return paths, nil
}

func wipeOutputFiles(files []outputFile) {
	for _, file := range files {
		clear(file.data)
	}
}

// printText escapes terminal controls only for a terminal, so a pipe or a file still gets the exact text.
func printText(environment *command.Environment, text string) {
	if environment.Terminal.StdoutIsTerminal() && strings.ContainsFunc(text, terminalControl) {
		fmt.Fprintf(
			environment.Stderr,
			"%s: the text holds terminal control characters, shown escaped; pipe the output to a file for the exact text\n",
			command.Program,
		)
		text = escapeTerminalControls(text)
	}
	fmt.Fprint(environment.Stdout, text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		fmt.Fprintln(environment.Stdout)
	}
}

// writeWithFallback tries a second new directory next to the first, so a failed write does not lose the message.
func writeWithFallback(environment *command.Environment, directory, base string, files []outputFile) ([]string, error) {
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
		"%s: warning: writing the files failed, trying a second new directory next to the first\n", command.Program,
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

func escapeTerminalControls(text string) string {
	var out strings.Builder
	for _, character := range text {
		if terminalControl(character) {
			fmt.Fprintf(&out, "\\u%04x", character)
			continue
		}
		out.WriteRune(character)
	}
	return out.String()
}

func outputDir(base string) (string, error) {
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		return "", errors.New("--out must name an existing directory")
	}
	directory, err := os.MkdirTemp(base, outputDirPrefix)
	if err != nil {
		return "", errors.New("--out: cannot create a private directory there")
	}
	return directory, nil
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
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, command.PrivateFileMode)
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
