package messages

import (
	"context"
	"crypto/hpke"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/cli/stopsignal"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/privatefile"
	"github.com/misaon/nylzo/internal/quarantine"
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
	errOutNotMarked = errors.New(
		"--out: the files there cannot be marked as downloaded from the internet; choose another directory",
	)
	errNotMarked    = errors.New("a file could not be marked as downloaded from the internet, so nylzo did not keep it")
	errStopTimedOut = errors.New(
		"the server did not send the message in time after the stop request, so read gave up; the open may be spent",
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
	keys       linkKeys
}

type linkKeys struct {
	R [core.SeedLen]byte
}

func (l *openLink) wipe() {
	clear(l.secret[:])
	clear(l.keys.R[:])
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
	if link.keys.R, err = keys.DeriveRead(link.id, &link.secret); err != nil {
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
	passphraseFlags(flags, &options.passphrase)
	flags.StringVar(
		&options.out,
		"out",
		"",
		"write the text and any attachment into a new private directory inside this existing directory",
	)
	flags.BoolVar(&options.stdout, "stdout", false, "print the text to stdout and write nothing to disk")
	return flags, options
}

func passphraseFlags(flags *flag.FlagSet, source *passphraseSource) {
	flags.StringVar(
		&source.file,
		"passphrase-file",
		"",
		"read the passphrase from the first line of this file (or "+command.EnvironmentPassphrase+")",
	)
	flags.BoolVar(&source.ask, "ask-passphrase", false, "ask for the passphrase on the terminal without echo")
}

// RunRead turns a closed pipe on stdout or stderr into a failed write, so it cannot end read after the open is spent;
// read exits with an error at the end instead. Notify, unlike Ignore, leaves the default for child processes.
func RunRead(ctx context.Context, environment *command.Environment, arguments []string) int {
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	var loss outputLoss
	watched := *environment
	watched.Stdout = &lossWatch{writer: environment.Stdout, loss: &loss}
	watched.Stderr = &lossWatch{writer: environment.Stderr, loss: &loss}
	guard := &claimGuard{environment: &watched}
	code := read(ctx, &watched, arguments, &loss, guard)
	guard.release()
	if code == command.ExitOK && loss.lost() != nil {
		code = command.ExitError
	}
	return code
}

type outputLoss struct {
	mutex sync.Mutex
	first error
}

func (l *outputLoss) record(err error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.first == nil {
		l.first = err
	}
}

func (l *outputLoss) lost() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.first
}

// lossWatch locks each stream on its own, since claimGuard warns from another goroutine and a stalled stderr must not
// hold up stdout.
type lossWatch struct {
	mutex  sync.Mutex
	writer io.Writer
	loss   *outputLoss
}

func (w *lossWatch) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	written, err := w.writer.Write(data)
	if err != nil {
		w.loss.record(err)
	}
	return written, err
}

// read makes the output directory after the passphrase prompt, which Ctrl-C ends before any cleanup, and before the
// claim, so a write problem never spends an open.
func read(
	ctx context.Context,
	environment *command.Environment,
	arguments []string,
	loss *outputLoss,
	guard *claimGuard,
) int {
	flags, options := readFlags(environment)
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if code, ok := checkDestination(ctx, environment, options); !ok {
		return code
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
	unlocked, err := unlockClaim(ctx, environment, server, &link, options.passphrase)
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	defer unlocked.wipe()
	var directory *os.Root
	if options.out != "" {
		if directory, err = outputDirectory(ctx, options.out); err != nil {
			return command.Failf(environment, "%s", err)
		}
		defer removeIfEmpty(directory)
	}
	guard.out = options.out
	guard.hangup = options.out != "" || !environment.Terminal.StdoutIsTerminal()
	opened, claimCtx, err := claimMessage(ctx, server, &link, unlocked, guard)
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	defer clear(opened.payload.File)
	paths, err := saveMessage(environment, directory, options.out, options.stdout, opened.payload)
	if err != nil {
		return command.Failf(environment, "the message was opened but could not be saved: %s", err)
	}
	if !options.stdout {
		for _, path := range paths {
			fmt.Fprintln(environment.Stdout, path)
		}
	}
	acknowledge(claimCtx, environment, server, &link, opened.lease)
	return finishRead(environment, paths, loss)
}

// checkDestination refuses a read without a destination, or with a --out that cannot take private files, before read
// asks for anything. The directory that proves --out goes again at once.
func checkDestination(ctx context.Context, environment *command.Environment, options *readOptions) (int, bool) {
	if !options.stdout && options.out == "" {
		fmt.Fprintln(environment.Stderr, command.Program+": "+errNoDestination.Error())
		return command.ExitUsage, false
	}
	if options.out == "" {
		return command.ExitOK, true
	}
	directory, err := outputDirectory(ctx, options.out)
	if err != nil {
		return command.Failf(environment, "%s", err), false
	}
	removeIfEmpty(directory)
	return command.ExitOK, true
}

func acknowledge(
	ctx context.Context,
	environment *command.Environment,
	server *client.Client,
	link *openLink,
	lease string,
) {
	if err := server.Ack(ctx, link.id, &link.keys.R, lease); err != nil && !client.IsKind(err, api.ErrorGone) {
		fmt.Fprintf(
			environment.Stderr,
			"%s: warning: the server did not confirm the read; it frees the message when the lease ends\n",
			command.Program,
		)
	}
}

func finishRead(environment *command.Environment, paths []string, loss *outputLoss) int {
	lost := loss.lost()
	if lost == nil {
		return command.ExitOK
	}
	if len(paths) == 0 {
		return command.Failf(environment, "part of the output could not be written: %s", lost)
	}
	return command.Failf(
		environment,
		"part of the output could not be written (%s); the files are in %s",
		lost,
		filepath.Dir(paths[0]),
	)
}

type openedMessage struct {
	payload envelope.Payload
	lease   string
}

// unlockedClaim holds what the handshake and the claim need; getting it spends nothing.
type unlockedClaim struct {
	metadata api.Metadata
	password []byte
}

func (u *unlockedClaim) wipe() {
	clear(u.password)
}

func unlockClaim(
	ctx context.Context,
	environment *command.Environment,
	server *client.Client,
	link *openLink,
	source passphraseSource,
) (*unlockedClaim, error) {
	metadata, err := server.Metadata(ctx, link.id, &link.keys.R)
	if err != nil {
		return nil, err
	}
	if checkErr := checkMetadata(link, &metadata); checkErr != nil {
		return nil, checkErr
	}
	unlocked := &unlockedClaim{metadata: metadata}
	if unlocked.password, err = readPassword(environment, link, source); err != nil {
		return nil, err
	}
	return unlocked, nil
}

func claimMessage(
	ctx context.Context,
	server *client.Client,
	link *openLink,
	unlocked *unlockedClaim,
	guard *claimGuard,
) (openedMessage, context.Context, error) {
	defer unlocked.wipe()
	if ctx.Err() != nil {
		return openedMessage{}, nil, context.Cause(ctx)
	}
	login, err := handshake(ctx, server, link, unlocked.password)
	if err != nil {
		return openedMessage{}, nil, err
	}
	defer login.Wipe()
	claimCtx, err := guard.hold(ctx, client.TransferTime(unlocked.metadata.Size))
	if err != nil {
		return openedMessage{}, nil, err
	}
	fetched, lease, err := fetchEnvelope(claimCtx, server, link, &login, unlocked.metadata.Size)
	if err != nil && errors.Is(context.Cause(claimCtx), errStopTimedOut) {
		return openedMessage{}, nil, errStopTimedOut
	}
	if err != nil {
		return openedMessage{}, nil, err
	}
	defer clear(fetched.Object)
	if !headerMatches(&fetched, link, &unlocked.metadata) {
		return openedMessage{}, nil, errPolicyChanged
	}
	contentKeys, err := contentKeysOf(link, &login.ExportKey, fetched.ShareKey, fetched.Share)
	if err != nil {
		return openedMessage{}, nil, errTampered
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
		return openedMessage{}, nil, errTampered
	}
	return openedMessage{payload: payload, lease: lease}, claimCtx, nil
}

// claimGuard keeps read running from the claim, which spends the open, until read returns: a hangup or a termination
// request (Go's SIGTERM for a closed Windows console) waits, the first Ctrl-C only warns and the second ends the
// process by it. After a stop request the claim and the Ack have one transfer time left; then further stop requests
// take effect at once. Held stop requests go to Environment.Stop, so the process ends by one after read.
type claimGuard struct {
	environment *command.Environment
	out         string
	hangup      bool
	held        chan os.Signal
	interrupt   chan os.Signal
	late        chan os.Signal
	lateSent    chan struct{}
	done        chan struct{}
	finished    chan struct{}
	grace       time.Duration
	cancelClaim context.CancelCauseFunc
	deadline    *time.Timer
	stopLate    func() bool
	stopped     bool
	interrupted bool
	warned      bool
}

func (g *claimGuard) hold(ctx context.Context, grace time.Duration) (context.Context, error) {
	g.held = make(chan os.Signal, 1)
	signal.Notify(g.held, g.heldSignals()...)
	g.interrupt = make(chan os.Signal, 2)
	stopsignal.Notify(g.interrupt, os.Interrupt)
	if ctx.Err() != nil {
		signal.Stop(g.held)
		signal.Stop(g.interrupt)
		return nil, context.Cause(ctx)
	}
	claimCtx, cancelClaim := context.WithCancelCause(context.WithoutCancel(ctx))
	g.grace, g.cancelClaim = grace, cancelClaim
	g.done, g.finished = make(chan struct{}), make(chan struct{})
	g.late, g.lateSent = make(chan os.Signal, 1), make(chan struct{})
	// A stop signal that reached only cli.Run, just before the registration above, is held like the others.
	g.stopLate = context.AfterFunc(ctx, func() {
		defer close(g.lateSent)
		if stop, ok := errors.AsType[stopsignal.StopError](context.Cause(ctx)); ok {
			g.late <- stop.Signal
		}
	})
	go g.watch()
	return claimCtx, nil
}

// heldSignals leaves a hangup alone when nohup ignores it, or when the only output is the terminal that hung up.
func (g *claimGuard) heldSignals() []os.Signal {
	held := []os.Signal{syscall.SIGTERM}
	if g.hangup && !signal.Ignored(syscall.SIGHUP) {
		held = append(held, syscall.SIGHUP)
	}
	return held
}

func (g *claimGuard) watch() {
	for {
		select {
		case received := <-g.held:
			g.stop(received)
		case received := <-g.late:
			g.stopLater(received)
		case received := <-g.interrupt:
			g.ctrlC(received)
		case <-g.done:
			for _, pending := range []chan os.Signal{g.held, g.interrupt, g.late} {
				select {
				case received := <-pending:
					g.stop(received)
				default:
				}
			}
			close(g.finished)
			return
		}
	}
}

func (g *claimGuard) ctrlC(received os.Signal) {
	if g.interrupted {
		stopsignal.Exit(received)
	}
	g.interrupted = true
	g.stop(received)
	g.warnOnce()
}

// stopLater does not count a late Ctrl-C: the guard may have received the same one, and counting it twice would end
// read before it saved the message.
func (g *claimGuard) stopLater(received os.Signal) {
	g.stop(received)
	if received == os.Interrupt {
		g.warnOnce()
	}
}

func (g *claimGuard) warnOnce() {
	if !g.warned {
		g.warned = true
		go g.warn()
	}
}

func (g *claimGuard) stop(received os.Signal) {
	g.environment.Stop.Hold(received)
	if !g.stopped {
		g.stopped = true
		g.deadline = time.AfterFunc(g.grace, func() {
			signal.Stop(g.held)
			signal.Reset(syscall.SIGTERM)
			g.cancelClaim(errStopTimedOut)
		})
	}
}

func (g *claimGuard) warn() {
	if g.out == "" {
		command.Warnf(
			g.environment,
			"opening the message spends an open, so read prints it before it stops, "+
				"though Ctrl-C may have stopped a program that reads its output; "+
				"a second Ctrl-C stops read at once and loses this open",
		)
		return
	}
	command.Warnf(
		g.environment,
		"opening the message spends an open, so read saves it before it stops; "+
			"a second Ctrl-C stops read at once, loses this open and may leave part of the message "+
			"in a nylzo- directory inside %s",
		g.out,
	)
}

func (g *claimGuard) release() {
	if g.done == nil {
		return
	}
	if !g.stopLate() {
		<-g.lateSent
	}
	signal.Stop(g.held)
	signal.Stop(g.interrupt)
	close(g.done)
	<-g.finished
	if g.deadline != nil {
		g.deadline.Stop()
	}
	g.cancelClaim(nil)
	g.done = nil
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

// checkBurnable refuses an ended message before the passphrase prompt; an opened one may still have its last open in
// flight, which a burn cuts off, so the server decides.
func checkBurnable(link *openLink, metadata *api.Metadata) error {
	if metadata.State != api.StateOpened || metadata.EndedAt != "" {
		return checkMetadata(link, metadata)
	}
	if err := core.AssertProfile(link.kdf, metadata.KDFID); err != nil {
		return errLinkMismatch
	}
	return nil
}

// headerMatches also compares a deadline the policy carries with the expiry of the metadata, which the envelope then
// authenticates (F13).
func headerMatches(fetched *client.Envelope, link *openLink, metadata *api.Metadata) bool {
	flagsOK := fetched.Flags == envelope.FlagDeadline || fetched.Flags == 0
	return flagsOK && fetched.KDF == link.kdf && int(fetched.Policy.OpensMax) == metadata.OpensMax &&
		int64(fetched.Policy.TTLSeconds) == metadata.TTLSeconds && deadlineMatches(fetched.Policy, metadata.ExpiresAt)
}

func deadlineMatches(policy envelope.Policy, expiresAt string) bool {
	if policy.Deadline == 0 {
		return true
	}
	expires, err := time.Parse(time.RFC3339, expiresAt)
	return err == nil && expires.Unix() == int64(policy.Deadline)
}

// contentKeysOf unwraps the share, which the server sends only with the object.
func contentKeysOf(
	link *openLink,
	exportKey *keys.ExportKey,
	shareKey hpke.PrivateKey,
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
	return keys.DeriveContent(link.id, &link.secret, exportKey, &expiryShare)
}

// logIn reads the passphrase and runs the handshake. A message without a passphrase runs it with the empty one.
func logIn(
	ctx context.Context,
	environment *command.Environment,
	server *client.Client,
	link *openLink,
	source passphraseSource,
) (client.Login, error) {
	password, err := readPassword(environment, link, source)
	if err != nil {
		return client.Login{}, err
	}
	defer clear(password)
	return handshake(ctx, server, link, password)
}

// readPassword reads the passphrase in secret mode, so it does not outlive its use where the build erases memory.
func readPassword(environment *command.Environment, link *openLink, source passphraseSource) ([]byte, error) {
	var (
		password []byte
		err      error
	)
	secretmode.Do(func() { password, err = opaquePassword(environment, link, source) })
	return password, err
}

// handshake runs OPAQUE in secret mode, so the scrypt memory does not outlive its use where the build erases memory.
func handshake(ctx context.Context, server *client.Client, link *openLink, password []byte) (client.Login, error) {
	var (
		login client.Login
		err   error
	)
	secretmode.Do(func() {
		var config opaque.Config
		if config, err = keys.OPAQUEConfig(link.id, link.kdf); err == nil {
			login, err = server.LogIn(ctx, link.id, &link.keys.R, link.kdf, password, &config)
		}
	})
	return login, err
}

func opaquePassword(environment *command.Environment, link *openLink, source passphraseSource) ([]byte, error) {
	if link.kdf == core.KDFNone {
		return keys.Password(&link.secret, link.kdf, "")
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
	return keys.Password(&link.secret, link.kdf, passphrase)
}

// fetchEnvelope resumes a broken claim through the lease it already received, so it never spends a second open.
func fetchEnvelope(
	ctx context.Context,
	server *client.Client,
	link *openLink,
	login *client.Login,
	size int64,
) (client.Envelope, string, error) {
	claimed, err := server.Claim(ctx, link.id, &link.keys.R, login, size, nil)
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
	files := outputFiles(payload, !toStdout)
	defer wipeOutputFiles(files)
	paths, err := writeOutput(environment, directory, base, files)
	if err == nil {
		printSaveNotices(environment, paths, toStdout, payload.Meta.File != nil)
	}
	if text := payload.Meta.Text; toStdout && text != nil {
		printText(environment, *text)
	}
	return paths, err
}

func outputFiles(payload envelope.Payload, withText bool) []outputFile {
	var files []outputFile
	if text := payload.Meta.Text; text != nil && withText {
		files = append(files, outputFile{textFileName, []byte(*text)})
	}
	if payload.Meta.File != nil {
		name := attachmentName(payload.Meta.File.Name)
		if sameFileName(name, textFileName) {
			name = attachmentPrefix + name
		}
		files = append(files, outputFile{name, payload.File})
	}
	return files
}

func writeOutput(
	environment *command.Environment,
	directory *os.Root,
	base string,
	files []outputFile,
) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if directory == nil {
		return nil, errAttachmentDiscarded
	}
	return writeWithFallback(environment, directory, base, files)
}

func printSaveNotices(environment *command.Environment, paths []string, toStdout, attached bool) {
	if toStdout {
		for _, path := range paths {
			fmt.Fprintf(environment.Stderr, "%s: the attachment is in %s\n", command.Program, path)
		}
	}
	if len(paths) > 0 {
		fmt.Fprintf(
			environment.Stderr,
			"%s: the files hold the plaintext; delete them when you no longer need them "+
				"(on SSDs and in backups a deletion may not erase them)\n", command.Program,
		)
	}
	if attached {
		fmt.Fprintf(
			environment.Stderr,
			"%s: nylzo cannot scan attachments for malware; open this one only if you trust the sender\n",
			command.Program,
		)
	}
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
	paths, err := writeAll(environment, directory, files)
	if err == nil {
		return paths, nil
	}
	fallback, fallbackErr := privatefile.MkdirTemp(base, outputDirectoryPrefix)
	if fallbackErr != nil {
		return nil, outputError(err)
	}
	defer removeIfEmpty(fallback)
	fmt.Fprintf(
		environment.Stderr,
		"%s: warning: writing the files failed, trying a second new directory next to the first\n", command.Program,
	)
	if paths, err = writeAll(environment, fallback, files); err != nil {
		return nil, outputError(err)
	}
	return paths, nil
}

func warnLeftovers(environment *command.Environment, err error) {
	left, found := errors.AsType[*leftoverError](err)
	if !found {
		return
	}
	for _, path := range left.paths {
		command.Warnf(
			environment,
			"could not remove %s after the failed write; it may hold all or part of the message in plaintext, "+
				"so delete it",
			path,
		)
	}
}

type leftoverError struct {
	error

	paths []string
}

func (e *leftoverError) Unwrap() error {
	return e.error
}

func removeAfter(err error, directory *os.Root, names ...string) error {
	var left []string
	for _, name := range names {
		if removeErr := directory.Remove(name); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			left = append(left, filepath.Join(directory.Name(), name))
		}
	}
	if len(left) == 0 {
		return err
	}
	if earlier, found := errors.AsType[*leftoverError](err); found {
		left = append(slices.Clone(earlier.paths), left...)
	}
	return &leftoverError{error: err, paths: left}
}

func outputError(err error) error {
	if errors.Is(err, errNotMarked) {
		return errNotMarked
	}
	return errWriteOutput
}

func writeAll(environment *command.Environment, directory *os.Root, files []outputFile) ([]string, error) {
	var names []string
	for _, file := range files {
		name := file.name
		err := writePrivate(directory, name, file.data)
		if errors.Is(err, fs.ErrExist) && !errors.Is(err, errNotMarked) && !strings.HasPrefix(name, attachmentPrefix) {
			name = attachmentPrefix + name
			err = writePrivate(directory, name, file.data)
		}
		if err != nil {
			err = removeAfter(err, directory, names...)
			warnLeftovers(environment, err)
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

// outputDirectory touches --out only while read may go on.
func outputDirectory(ctx context.Context, base string) (*os.Root, error) {
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		return nil, errOutNotDirectory
	}
	directory, err := privatefile.MkdirTemp(base, outputDirectoryPrefix)
	if err == nil {
		err = probeOutput(directory)
	}
	switch {
	case errors.Is(err, privatefile.ErrNotPrivate):
		return nil, errOutNotPrivate
	case errors.Is(err, errNotMarked):
		return nil, errOutNotMarked
	case err != nil:
		return nil, errOutNotCreated
	default:
		return directory, nil
	}
}

// probeOutput proves a private, marked file can be made there before the open is spent: FAT keeps one mask for
// directories and another for files.
func probeOutput(directory *os.Root) error {
	err := writePrivate(directory, probeFileName, nil)
	if err == nil {
		err = directory.Remove(probeFileName)
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

// writePrivate creates a new file only inside the directory, never replaces one, and marks it as downloaded from the
// internet before it holds any content.
func writePrivate(directory *os.Root, name string, data []byte) error {
	file, err := privatefile.CreateIn(directory, name)
	if err != nil {
		return err
	}
	if err = quarantine.Mark(file); err != nil {
		err = fmt.Errorf("%w: %w", errNotMarked, err)
	} else {
		_, err = file.Write(data)
	}
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		return removeAfter(err, directory, name)
	}
	return nil
}
