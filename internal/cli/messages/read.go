package messages

import (
	"context"
	"crypto/ecdh"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/privatefile"
	"github.com/misaon/nylzo/internal/secretmode"
)

const (
	textFileName          = "message.txt"
	blobRetries           = 3
	outputDirectoryPrefix = "nylzo-"
	attachmentPrefix      = "attachment-"
	probeFileName         = "probe"
	releasesURL           = "https://github.com/misaon/nylzo/releases"
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
	errLinkNewerVersion = errors.New("the link was made by a newer version of nylzo; update nylzo from " + releasesURL)
	errLinkReleased     = errors.New(
		"the link is from before this instance's security upgrade, which deleted every message it held; " +
			"ask the sender for a new link",
	)
	errLinkRetiredProfile = errors.New(
		"the link uses a passphrase profile that nylzo no longer supports; ask the sender for a new link",
	)
	errLinkDamaged       = errors.New("the link is incomplete or damaged")
	errLinkOtherInstance = errors.New(
		"the link belongs to another instance than --server or " + command.EnvironmentServer,
	)
	errLinkMismatch     = errors.New("the link does not match the message on the server")
	errPassphraseNeeded = errors.New(
		"this message needs a passphrase: use --passphrase-file, " + command.EnvironmentPassphrase + " or --ask-passphrase",
	)
	errOutNotDirectory = errors.New("--out must name an existing directory")
	errOutNotCreated   = errors.New("--out: cannot create a private directory there")
	errOutNotPrivate   = errors.New(
		"--out: the file system there cannot keep files private; choose a directory on a local disk",
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
		return openLink{}, errLinkNewerVersion
	case errors.Is(err, core.ErrLinkReleased):
		return openLink{}, errLinkReleased
	case errors.Is(err, core.ErrLinkRetiredKDF):
		return openLink{}, errLinkRetiredProfile
	case err != nil:
		return openLink{}, errLinkDamaged
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
	raw, err := ReadLink(environment, linkFile)
	if err != nil {
		return openLink{}, err
	}
	return parseOpenLink(raw)
}

type HandedOver struct {
	Path     string
	Fragment string
}

func HandOver(raw, instance string) (HandedOver, error) {
	parsed, err := parseOpenLink(raw)
	if err != nil {
		return HandedOver{}, err
	}
	parsed.wipe()
	if parsed.origin != instance {
		return HandedOver{}, errLinkOtherInstance
	}
	head, fragment, _ := strings.Cut(raw, "#")
	address, err := url.Parse(head)
	if err != nil {
		return HandedOver{}, errLinkDamaged
	}
	return HandedOver{Path: address.EscapedPath(), Fragment: fragment}, nil
}

func readFlags(environment *command.Environment) (*flag.FlagSet, *readOptions) {
	options := &readOptions{}
	flags := command.NewFlags(environment, "read")
	flags.StringVar(
		&options.linkFile,
		"link-file",
		"",
		"read the link from this file (or "+command.EnvironmentLink+", or stdin)",
	)
	flags.StringVar(
		&options.passphrase.file,
		"passphrase-file",
		"",
		"read the passphrase from the first line of this file (or "+command.EnvironmentPassphrase+")",
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

// RunRead creates the output directory before the claim, so a write problem never spends an open.
func RunRead(ctx context.Context, environment *command.Environment, arguments []string) int {
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
		return command.Failf(environment, "link: %s", command.Explain(err))
	}
	defer link.wipe()
	server, err := client.New(link.origin)
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	var directory *os.Root
	if options.out != "" {
		if directory, err = outputDirectory(options.out); err != nil {
			return command.Failf(environment, "%s", err)
		}
		defer removeIfEmpty(directory)
	}
	opened, err := readMessage(ctx, environment, server, &link, options.passphrase)
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	defer clear(opened.payload.File)
	paths, err := saveMessage(environment, directory, options.out, options.stdout, opened.payload)
	if err != nil {
		return command.Failf(environment, "the message was opened but could not be saved: %s", err)
	}
	if err = server.Ack(ctx, link.id, &link.keys.R, opened.lease); err != nil && !client.IsKind(err, api.ErrorGone) {
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
	server *client.Client,
	link *openLink,
	source passphraseSource,
) (openedMessage, error) {
	metadata, err := server.Metadata(ctx, link.id, &link.keys.R)
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
	fetched, lease, err := fetchEnvelope(ctx, server, link, &verifier, metadata.Size)
	if err != nil {
		return openedMessage{}, err
	}
	defer clear(fetched.Object)
	if !headerMatches(&fetched, link, &metadata) {
		return openedMessage{}, errPolicyChanged
	}
	contentKeys, err := contentKeysOf(link, passphraseKey, fetched.ShareKey, fetched.Share)
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
func checkMetadata(link *openLink, metadata *api.Metadata) error {
	if metadata.State != api.StateAvailable {
		return &client.APIError{Status: http.StatusGone, Kind: api.ErrorGone, State: metadata.State}
	}
	if err := core.AssertProfile(link.kdf, metadata.KDFID); err != nil {
		return errLinkMismatch
	}
	return nil
}

func headerMatches(fetched *client.Envelope, link *openLink, metadata *api.Metadata) bool {
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
	if wrapped == nil || shareKey == nil {
		return keys.ContentKeys{}, errTampered
	}
	expiryShare, err := share.Unwrap(shareKey, wrapped)
	if err != nil {
		return keys.ContentKeys{}, err
	}
	defer clear(expiryShare[:])
	return keys.DeriveContent(link.id, &link.secret, passphraseKey, &expiryShare)
}

// passphraseKey reads the passphrase and runs scrypt in secret mode, so neither the passphrase nor the scrypt memory
// outlives its use where the build erases memory.
func passphraseKey(environment *command.Environment, link *openLink, source passphraseSource) ([]byte, error) {
	var (
		key []byte
		err error
	)
	secretmode.Do(func() { key, err = readAndDerivePassphraseKey(environment, link, source) })
	return key, err
}

func readAndDerivePassphraseKey(
	environment *command.Environment,
	link *openLink,
	source passphraseSource,
) ([]byte, error) {
	if link.kdf == core.KDFNone {
		return nil, nil
	}
	if !source.given(environment) {
		source.ask = true
	}
	passphrase, err := readPassphrase(environment, source, false)
	if errors.Is(err, console.ErrNoTerminal) {
		return nil, errPassphraseNeeded
	}
	if err != nil {
		return nil, err
	}
	return keys.DerivePassphraseKey(link.kdf, passphrase, &link.keys.PassphraseSalt)
}

// fetchEnvelope resumes a broken claim through the lease it already received, so it never spends a second open.
func fetchEnvelope(
	ctx context.Context,
	server *client.Client,
	link *openLink,
	verifier *[core.SeedLen]byte,
	size int64,
) (client.Envelope, string, error) {
	claimed, err := server.Claim(ctx, link.id, &link.keys.R, verifier, size, nil)
	if err == nil {
		return claimed.Envelope, claimed.Lease, nil
	}
	if claimed.Lease == "" {
		return client.Envelope{}, "", err
	}
	for range blobRetries {
		fetched, blobErr := server.Blob(ctx, link.id, &link.keys.R, claimed.Lease, size)
		if blobErr == nil {
			return fetched, claimed.Lease, nil
		}
		err = blobErr
		if client.IsKind(blobErr, api.ErrorGone) || client.IsKind(blobErr, api.ErrorRateLimited) || ctx.Err() != nil {
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
	directory *os.Root,
	base string,
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
	if directory == nil {
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
		text = command.EscapeRunes(text, terminalControl)
	}
	fmt.Fprint(environment.Stdout, text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		fmt.Fprintln(environment.Stdout)
	}
}

// writeWithFallback tries a second new directory next to the first, so a failed write does not lose the message.
func writeWithFallback(
	environment *command.Environment,
	directory *os.Root,
	base string,
	files []outputFile,
) ([]string, error) {
	paths, err := writeAll(directory, files)
	if err == nil {
		return paths, nil
	}
	fallback, err := privatefile.MkdirTemp(base, outputDirectoryPrefix)
	if err != nil {
		return nil, errWriteOutput
	}
	defer removeIfEmpty(fallback)
	fmt.Fprintf(
		environment.Stderr,
		"%s: warning: writing the files failed, trying a second new directory next to the first\n", command.Program,
	)
	if paths, err = writeAll(fallback, files); err != nil {
		return nil, errWriteOutput
	}
	return paths, nil
}

func writeAll(directory *os.Root, files []outputFile) ([]string, error) {
	var names []string
	for _, file := range files {
		name := file.name
		err := writePrivate(directory, name, file.data)
		if errors.Is(err, fs.ErrExist) && !strings.HasPrefix(name, attachmentPrefix) {
			name = attachmentPrefix + name
			err = writePrivate(directory, name, file.data)
		}
		if err != nil {
			for _, written := range names {
				_ = directory.Remove(written)
			}
			return nil, err
		}
		names = append(names, name)
	}
	paths := make([]string, len(names))
	for index, name := range names {
		paths[index] = filepath.Join(directory.Name(), name)
	}
	return paths, nil
}

func outputDirectory(base string) (*os.Root, error) {
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		return nil, errOutNotDirectory
	}
	directory, err := privatefile.MkdirTemp(base, outputDirectoryPrefix)
	if err == nil {
		err = probePrivateFile(directory)
	}
	if errors.Is(err, privatefile.ErrNotPrivate) {
		return nil, errOutNotPrivate
	}
	if err != nil {
		return nil, errOutNotCreated
	}
	return directory, nil
}

// probePrivateFile proves a private file can be made there before the open is spent: FAT keeps one mask for
// directories and another for files.
func probePrivateFile(directory *os.Root) error {
	file, err := privatefile.CreateIn(directory, probeFileName)
	if err == nil {
		err = errors.Join(file.Close(), directory.Remove(probeFileName))
	}
	if err != nil {
		removeIfEmpty(directory)
	}
	return err
}

// removeIfEmpty closes the directory first, since Windows refuses to remove a directory that is still open.
func removeIfEmpty(directory *os.Root) {
	entries, err := fs.ReadDir(directory.FS(), ".")
	directory.Close()
	if err == nil && len(entries) == 0 {
		os.Remove(directory.Name())
	}
}

// writePrivate creates a new file only inside the directory and never replaces one.
func writePrivate(directory *os.Root, name string, data []byte) error {
	file, err := privatefile.CreateIn(directory, name)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		_ = directory.Remove(name)
		return err
	}
	return nil
}
