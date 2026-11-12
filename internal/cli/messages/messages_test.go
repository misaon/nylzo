package messages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/cli/stopsignal"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

type noTerminal struct{}

func (noTerminal) StdinIsTerminal() bool { return false }

func (noTerminal) StdoutIsTerminal() bool { return false }

func (noTerminal) ReadSecret(string) ([]byte, error) { return nil, console.ErrNoTerminal }

type testServer struct {
	url string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	return serveWith(t, func(*http.Request) {})
}

func serveWith(t *testing.T, before func(*http.Request)) *testServer {
	t.Helper()
	directory := t.TempDir()
	reportKey, err := abuse.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Defaults()
	settings.DataDirectory = directory
	settings.Pepper = bytes.Repeat([]byte{7}, 32)
	settings.ReportKey = reportKey.PublicKey()
	settings.Development.AllowUnerasableStorage = runtime.GOOS != "linux"
	settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 1<<20, 1<<20
	settings.Rates.SourcePerSecond, settings.Rates.SourceBurst = 1<<20, 1<<20
	settings.Rates.CreatePerHour, settings.Rates.CreatePerDay, settings.Rates.ReadPerHour = 1000, 1000, 1000
	storage, err := sqlite.Open(t.Context(), filepath.Join(directory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	blobs, err := disk.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := server.New(&settings, storage, blobs, server.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	handler := apiServer.HTTPServer().Handler
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		before(request)
		handler.ServeHTTP(writer, request)
	}))
	t.Cleanup(httpServer.Close)
	t.Cleanup(apiServer.Close)
	return &testServer{url: httpServer.URL}
}

type commandResult struct {
	code   int
	stdout string
	stderr string
}

var commandsByName = map[string]func(context.Context, *command.Environment, []string) int{
	"create": RunCreate,
	"read":   RunRead,
}

func runWith(t *testing.T, stdin string, arguments ...string) commandResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := commandsByName[arguments[0]](t.Context(), &command.Environment{
		Stdin:    strings.NewReader(stdin),
		Stdout:   &stdout,
		Stderr:   &stderr,
		Getenv:   func(string) string { return "" },
		Terminal: noTerminal{},
		Version:  "test",
	}, arguments[1:])
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func createText(t *testing.T, instance *testServer, text string, arguments ...string) string {
	t.Helper()
	result := runWith(t, text, append([]string{"create", "--server", instance.url}, arguments...)...)
	if result.code != command.ExitOK {
		t.Fatalf("create: exit %d, %s", result.code, result.stderr)
	}
	link, _, _ := strings.Cut(result.stdout, "\n")
	return link
}

func createWithFile(t *testing.T, instance *testServer, text, name string, data []byte) string {
	t.Helper()
	apiClient, err := client.New(instance.url)
	if err != nil {
		t.Fatal(err)
	}
	message := content{
		meta: envelope.Meta{Text: &text, File: &envelope.FileMeta{Name: name, Size: int64(len(data))}},
		file: data,
	}
	created, err := createOnce(
		t.Context(),
		&command.Environment{Stderr: io.Discard},
		apiClient,
		&createInput{
			options:   &createOptions{opens: 1},
			message:   &message,
			policy:    envelope.Policy{OpensMax: 1, TTLSeconds: 3600},
			maxObject: envelope.ObjectLimit(envelope.MaxFileBytes),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return created.link
}

func readPaths(t *testing.T, result commandResult) []string {
	t.Helper()
	if result.code != command.ExitOK {
		t.Fatalf("read: exit %d, %s", result.code, result.stderr)
	}
	return strings.Fields(result.stdout)
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertPrivate(t *testing.T, path, want string) {
	t.Helper()
	if got := mustRead(t, path); got != want {
		t.Fatalf("%s holds %q, want %q", path, got, want)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for _, privatePath := range []string{path, filepath.Dir(path)} {
		info, err := os.Stat(privatePath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is open to other users: %v", privatePath, info.Mode().Perm())
		}
	}
}

// F05: a burn leaves an opened message whose last open may still run to the server and refuses an ended one.
func TestBurnChecksTheMessageFirst(t *testing.T) {
	t.Parallel()
	link := &openLink{kdf: core.KDFScrypt}
	for _, testCase := range []struct {
		metadata api.Metadata
		want     error
	}{
		{api.Metadata{State: api.StateAvailable, KDFID: int(core.KDFScrypt)}, nil},
		{api.Metadata{State: api.StateOpened, KDFID: int(core.KDFScrypt)}, nil},
		{api.Metadata{State: api.StateOpened, KDFID: int(core.KDFNone)}, errLinkMismatch},
		{api.Metadata{State: api.StateAvailable, KDFID: int(core.KDFNone)}, errLinkMismatch},
	} {
		if err := checkBurnable(link, &testCase.metadata); !errors.Is(err, testCase.want) {
			t.Errorf("%+v: %v, want %v", testCase.metadata, err, testCase.want)
		}
	}
	for _, ended := range []api.Metadata{
		{State: api.StateOpened, EndedAt: "2026-11-11T10:00:00Z"},
		{State: api.StateExpired},
		{State: api.StateDeleted},
	} {
		if !client.IsKind(checkBurnable(link, &ended), api.ErrorGone) {
			t.Errorf("%+v must be refused as gone", ended)
		}
	}
}

func TestManageTokenFormat(t *testing.T) {
	t.Parallel()
	id := core.NewID()
	seed := core.NewSecret()
	good := formatManageToken("https://nylzo.example", id, seed)
	token, err := parseManageToken(good)
	if err != nil || token.origin != "https://nylzo.example" || token.id != id || token.seed != *seed {
		t.Fatalf("round trip: %+v %v", token, err)
	}
	for _, bad := range []string{
		"", good + "extra\n", "https://nylzo.example/m/" + id.String() + "#1.0.x\n" + core.Encode(seed[:]),
		"http://nylzo.example/m/" + id.String() + "\n" + core.Encode(seed[:]), "https://nylzo.example/x/" + id.String() + "\n" + core.Encode(seed[:]),
		"https://nylzo.example/m/" + id.String() + "\nshort",
	} {
		if _, err = parseManageToken(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func openRoot(t *testing.T, path string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func TestWritePrivateNeverOverwrites(t *testing.T) {
	t.Parallel()
	directory := openRoot(t, t.TempDir())
	if err := writePrivate(directory, "a.txt", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(directory, "a.txt", []byte("two")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrote an existing file: %v", err)
	}
	if got := mustRead(t, filepath.Join(directory.Name(), "a.txt")); got != "one" {
		t.Fatalf("file now holds %q", got)
	}
}

func TestReadNeverLosesAMessageToANameClash(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	out := t.TempDir()
	first := createText(t, instance, "first")
	second := createText(t, instance, "second")
	firstPaths := readPaths(t, runWith(t, first, "read", "--out", out))
	secondPaths := readPaths(t, runWith(t, second, "read", "--out", out))
	assertPrivate(t, firstPaths[0], "first")
	assertPrivate(t, secondPaths[0], "second")
	apiClient, err := client.New(instance.url)
	if err != nil {
		t.Fatal(err)
	}
	text := "beside a clashing attachment"
	message := content{
		meta: envelope.Meta{Text: &text, File: &envelope.FileMeta{Name: "MESSAGE.TXT", MIME: "text/plain", Size: 4}},
		file: []byte("file"),
	}
	created, err := createOnce(
		t.Context(),
		&command.Environment{Stderr: io.Discard},
		apiClient,
		&createInput{
			options:   &createOptions{opens: 1},
			message:   &message,
			policy:    envelope.Policy{OpensMax: 1, TTLSeconds: 3600},
			maxObject: envelope.ObjectLimit(envelope.MaxFileBytes),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	paths := readPaths(t, runWith(t, created.link, "read", "--out", out))
	if len(paths) != 2 || filepath.Base(paths[0]) != textFileName ||
		filepath.Base(paths[1]) != "attachment-MESSAGE.TXT" {
		t.Fatalf("text and attachment: %v", paths)
	}
	assertPrivate(t, paths[0], text)
	assertPrivate(t, paths[1], "file")
}

func TestWritePrivateStaysInItsDirectory(t *testing.T) {
	t.Parallel()
	directory := openRoot(t, t.TempDir())
	for _, name := range []string{"../escape", "sub/../../escape"} {
		if err := writePrivate(directory, name, []byte("x")); err == nil {
			t.Fatalf("wrote %q outside %s", name, directory.Name())
		}
	}
}

func TestNamesThatFoldTogetherDoNotClash(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	apiClient, err := client.New(instance.url)
	if err != nil {
		t.Fatal(err)
	}
	text := "beside a folded clash"
	for _, name := range []string{"Meßage.txt", "meſſage.txt", "MESSAGE.TXT"} {
		message := content{
			meta: envelope.Meta{Text: &text, File: &envelope.FileMeta{Name: name, MIME: "text/plain", Size: 4}},
			file: []byte("file"),
		}
		created, createErr := createOnce(
			t.Context(),
			&command.Environment{Stderr: io.Discard},
			apiClient,
			&createInput{
				options:   &createOptions{opens: 1},
				message:   &message,
				policy:    envelope.Policy{OpensMax: 1, TTLSeconds: 3600},
				maxObject: envelope.ObjectLimit(envelope.MaxFileBytes),
			},
		)
		if createErr != nil {
			t.Fatal(createErr)
		}
		paths := readPaths(t, runWith(t, created.link, "read", "--out", t.TempDir()))
		if len(paths) != 2 || !strings.HasPrefix(filepath.Base(paths[1]), attachmentPrefix) {
			t.Fatalf("%s: %v", name, paths)
		}
		assertPrivate(t, paths[1], "file")
	}
	directory := t.TempDir()
	if err = os.WriteFile(filepath.Join(directory, "taken.bin"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := writeAll(
		&command.Environment{Stderr: io.Discard},
		openRoot(t, directory),
		[]outputFile{{"taken.bin", []byte("new")}},
	)
	if err != nil || filepath.Base(paths[0]) != attachmentPrefix+"taken.bin" {
		t.Fatalf("an existing name was not avoided: %v %v", paths, err)
	}
}

func TestFallbackStaysInsideOut(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	t.Parallel()
	base := t.TempDir()
	locked := filepath.Join(base, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	text := "after a failed write"
	var stderr bytes.Buffer
	paths, err := saveMessage(
		&command.Environment{Stdout: io.Discard, Stderr: &stderr, Terminal: noTerminal{}},
		openRoot(t, locked),
		base,
		false,
		envelope.Payload{Meta: envelope.Meta{Text: &text}},
	)
	if err != nil || len(paths) != 1 || filepath.Dir(filepath.Dir(paths[0])) != base ||
		filepath.Dir(paths[0]) == locked {
		t.Fatalf("fallback wrote %v (%v)", paths, err)
	}
	assertPrivate(t, paths[0], text)
	full := filepath.Join(t.TempDir(), "full")
	if err = os.Mkdir(full, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err = saveMessage(
		&command.Environment{Stdout: io.Discard, Stderr: io.Discard, Terminal: noTerminal{}},
		openRoot(t, full),
		full,
		false,
		envelope.Payload{Meta: envelope.Meta{Text: &text}},
	); err == nil {
		t.Fatal("a write with nowhere to go succeeded")
	}
	partial := t.TempDir()
	if err = os.Mkdir(filepath.Join(partial, "a.bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(partial, attachmentPrefix+"a.bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err = writeAll(
		&command.Environment{Stderr: io.Discard},
		openRoot(t, partial),
		[]outputFile{{textFileName, []byte("x")}, {"a.bin", []byte("y")}},
	); err == nil {
		t.Fatal("writeAll succeeded with nowhere to put the attachment")
	}
	if _, err = os.Stat(filepath.Join(partial, textFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a failed set of files left the text behind")
	}
}

type recordingStdout struct {
	bytes.Buffer

	beforeFirstWrite func()
}

func (r *recordingStdout) Write(data []byte) (int, error) {
	if r.beforeFirstWrite != nil {
		r.beforeFirstWrite()
		r.beforeFirstWrite = nil
	}
	return r.Buffer.Write(data)
}

func notesPayload(text *string) envelope.Payload {
	return envelope.Payload{
		Meta: envelope.Meta{Text: text, File: &envelope.FileMeta{Name: "notes.bin", Size: 1}},
		File: []byte{1},
	}
}

func readWith(t *testing.T, link string, stdout, stderr io.Writer, arguments ...string) int {
	t.Helper()
	return RunRead(t.Context(), readEnvironment(link, stdout, stderr), arguments)
}

func readEnvironment(link string, stdout, stderr io.Writer) *command.Environment {
	return &command.Environment{
		Stdin:    strings.NewReader(link),
		Stdout:   stdout,
		Stderr:   stderr,
		Getenv:   func(string) string { return "" },
		Terminal: noTerminal{},
		Version:  "test",
	}
}

func savedAttachments(t *testing.T, out string) []string {
	t.Helper()
	saved, err := filepath.Glob(filepath.Join(out, outputDirectoryPrefix+"*", "*.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestStdoutGetsOnlyTheTextAfterTheFileAndTheWarnings(t *testing.T) {
	t.Parallel()
	text := "beside a file"
	link := createWithFile(t, newTestServer(t), text, "notes.bin", []byte{1})
	out := t.TempDir()
	var stderr bytes.Buffer
	var warnedFirst string
	var savedFirst []string
	stdout := &recordingStdout{beforeFirstWrite: func() {
		warnedFirst = stderr.String()
		savedFirst = savedAttachments(t, out)
	}}
	code := readWith(t, link, stdout, &stderr, "--stdout", "--out", out)
	if code != command.ExitOK || stdout.String() != text+"\n" {
		t.Fatalf("read: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	if len(savedFirst) != 1 {
		t.Fatalf("the text reached stdout before the file was saved: %v", savedFirst)
	}
	for _, notice := range []string{"the attachment is in", "hold the plaintext", "cannot scan attachments"} {
		if !strings.Contains(warnedFirst, notice) {
			t.Fatalf("the text reached stdout before %q: %q", notice, warnedFirst)
		}
	}
}

func TestStdoutAloneStillPrintsTheTextBesideAFileItCannotSave(t *testing.T) {
	t.Parallel()
	text := "the file stays behind"
	var stdout, stderr bytes.Buffer
	_, err := saveMessage(
		&command.Environment{Stdout: &stdout, Stderr: &stderr, Terminal: noTerminal{}},
		nil,
		"",
		true,
		notesPayload(&text),
	)
	if !errors.Is(err, errAttachmentDiscarded) || stdout.String() != text+"\n" ||
		strings.Contains(stderr.String(), "scan") {
		t.Fatalf("save: %v, stdout %q, stderr %q", err, stdout.String(), stderr.String())
	}
}

type closedWriter struct{}

func (closedWriter) Write([]byte) (int, error) {
	return 0, errors.New("closed")
}

func TestAClosedOutputFailsTheReadButKeepsTheFile(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	for _, test := range []struct {
		closed    string
		arguments []string
		open      []string
	}{
		{"stdout", []string{"--stdout"}, []string{"cannot scan attachments", "the files are in"}},
		{"stdout", nil, []string{"cannot scan attachments", "the files are in"}},
		{"stderr", nil, []string{textFileName, "notes.bin"}},
	} {
		link := createWithFile(t, instance, "one stream is gone", "notes.bin", []byte{1})
		out := t.TempDir()
		var open bytes.Buffer
		stdout, stderr := io.Writer(closedWriter{}), io.Writer(&open)
		if test.closed == "stderr" {
			stdout, stderr = &open, closedWriter{}
		}
		code := readWith(t, link, stdout, stderr, append(test.arguments, "--out", out)...)
		if saved := savedAttachments(t, out); code != command.ExitError || len(saved) != 1 {
			t.Fatalf("closed %s %v: exit %d, attachment %v", test.closed, test.arguments, code, saved)
		}
		for _, want := range test.open {
			if !strings.Contains(open.String(), want) {
				t.Fatalf(
					"closed %s %v: the other stream lacks %q: %q",
					test.closed,
					test.arguments,
					want,
					open.String(),
				)
			}
		}
	}
}

func TestHelpIntoAClosedPipeFails(t *testing.T) {
	t.Parallel()
	if code := readWith(t, "", closedWriter{}, io.Discard, "-h"); code != command.ExitError {
		t.Fatalf("help that went nowhere: exit %d", code)
	}
}

const (
	closedPipesChild     = "NYLZO_TEST_CLOSED_PIPES"
	stopSignalsChild     = "NYLZO_TEST_STOP_SIGNALS"
	secondInterruptChild = "NYLZO_TEST_SECOND_INTERRUPT"
	afterGraceChild      = "NYLZO_TEST_AFTER_GRACE"
	nohupChild           = "NYLZO_TEST_NOHUP"
)

func TestReadOutlivesClosedOutputPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no SIGPIPE")
	}
	if os.Getenv(closedPipesChild) != "" {
		readIntoClosedPipes(t)
		return
	}
	t.Parallel()
	verdict := childVerdict(t, "TestReadOutlivesClosedOutputPipes", closedPipesChild, true)
	if verdict != "saved" {
		t.Fatalf("the child reported %q", verdict)
	}
}

func readIntoClosedPipes(t *testing.T) {
	t.Helper()
	link := createWithFile(t, newTestServer(t), "into a closed pipe", "notes.bin", []byte{1})
	out := t.TempDir()
	code := readWith(t, link, os.Stdout, os.Stderr, "--stdout", "--out", out)
	verdict := "saved"
	if saved := savedAttachments(t, out); code != command.ExitError || len(saved) != 1 {
		verdict = fmt.Sprintf("exit %d, attachment %v", code, saved)
	}
	reportVerdict(t, verdict)
}

// A hangup, a termination request or a first Ctrl-C after the claim must not end read before the message is saved.
func TestReadSavesTheMessageThroughStopSignalsAfterTheClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no SIGHUP")
	}
	skipIfCtrlCIsIgnored(t)
	if os.Getenv(stopSignalsChild) != "" {
		readThroughStopSignals(t)
		return
	}
	t.Parallel()
	verdict := childVerdict(t, "TestReadSavesTheMessageThroughStopSignalsAfterTheClaim", stopSignalsChild, false)
	if verdict != "saved saved" {
		t.Fatalf("the child reported %q", verdict)
	}
}

// readThroughStopSignals cancels the context as cli.Run does on a first Ctrl-C or SIGTERM, sends the process a hangup,
// a termination request and a Ctrl-C while the claim is on its way to the server, and with --stdout sends the first two
// again while read prints the text.
func readThroughStopSignals(t *testing.T) {
	t.Helper()
	var verdicts []string
	for _, arguments := range [][]string{nil, {"--stdout"}} {
		ctx, stop := context.WithCancel(t.Context())
		instance := serveWith(t, func(request *http.Request) {
			if strings.HasSuffix(request.URL.Path, api.ClaimAction) {
				stop()
				signalSelf(t, syscall.SIGHUP, syscall.SIGTERM, os.Interrupt)
			}
		})
		text := "after the claim"
		link := createWithFile(t, instance, text, "signals.bin", []byte{1})
		out := t.TempDir()
		stdout := &recordingStdout{}
		if arguments != nil {
			stdout.beforeFirstWrite = func() { signalSelf(t, syscall.SIGHUP, syscall.SIGTERM) }
		}
		stderr := &lockedBuffer{}
		environment := readEnvironment(link, stdout, stderr)
		environment.Stop = new(stopsignal.Pending)
		code := RunRead(ctx, environment, append(arguments, "--out", out))
		stop()
		saved := savedAttachments(t, out)
		got := deliveredText(arguments, stdout.String(), saved)
		verdict := "saved"
		held := environment.Stop.Signal()
		if code != command.ExitOK || !environment.Stop.Held() || held != os.Interrupt || len(saved) != 1 ||
			got != text ||
			!strings.Contains(
				stderr.String(),
				"a second Ctrl-C",
			) || strings.Contains(stderr.String(), "did not confirm") {
			verdict = fmt.Sprintf(
				"%v: exit %d, held %v, attachment %v, text %q, stderr %q",
				arguments,
				code,
				held,
				saved,
				got,
				stderr.String(),
			)
		}
		verdicts = append(verdicts, verdict)
	}
	reportVerdict(t, strings.Join(verdicts, " "))
}

type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer

	afterWrite func(string)
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	written, err := b.buffer.Write(data)
	b.mutex.Unlock()
	if b.afterWrite != nil {
		b.afterWrite(string(data))
	}
	return written, err
}

func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

// A second Ctrl-C after the claim ends read at once by that Ctrl-C, as the warning after the first one says.
func TestASecondCtrlCAfterTheClaimStopsRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test sends itself signals")
	}
	skipIfCtrlCIsIgnored(t)
	if os.Getenv(secondInterruptChild) != "" {
		interruptReadTwice(t)
		return
	}
	t.Parallel()
	child := runChild(t, "TestASecondCtrlCAfterTheClaimStopsRead", secondInterruptChild, false)
	if child.code != -1 || child.signal != syscall.SIGINT {
		t.Fatalf("exit %d, signal %v after a second Ctrl-C\n%s", child.code, child.signal, child.output)
	}
}

func interruptReadTwice(t *testing.T) {
	t.Helper()
	instance := serveWith(t, func(request *http.Request) {
		if strings.HasSuffix(request.URL.Path, api.ClaimAction) {
			signalSelf(t, os.Interrupt)
		}
	})
	link := createWithFile(t, instance, "never saved", "signals.bin", []byte{1})
	stderr := &lockedBuffer{afterWrite: func(written string) {
		if strings.Contains(written, "a second Ctrl-C") {
			signalSelf(t, os.Interrupt)
		}
	}}
	code := RunRead(t.Context(), readEnvironment(link, io.Discard, stderr), []string{"--out", t.TempDir()})
	t.Fatalf("read returned %d after a second Ctrl-C: %q", code, stderr.String())
}

func skipIfCtrlCIsIgnored(t *testing.T) {
	t.Helper()
	if signal.Ignored(os.Interrupt) {
		t.Skip("the tests run with Ctrl-C ignored, which read then leaves ignored")
	}
}

func signalSelf(t *testing.T, signals ...os.Signal) {
	t.Helper()
	self, err := os.FindProcess(os.Getpid())
	for _, sent := range signals {
		if err == nil {
			err = self.Signal(sent)
		}
	}
	if err != nil {
		t.Error(err)
	}
}

type stalledWriter struct {
	entered chan<- struct{}
	release <-chan struct{}
}

func (w stalledWriter) Write(data []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return len(data), nil
}

func TestAStalledStderrDoesNotHoldUpStdout(t *testing.T) {
	t.Parallel()
	var loss outputLoss
	entered, release := make(chan struct{}, 1), make(chan struct{})
	defer close(release)
	stderr := &lossWatch{writer: stalledWriter{entered: entered, release: release}, loss: &loss}
	stdout := &lossWatch{writer: io.Discard, loss: &loss}
	go func() { _, _ = stderr.Write([]byte("warning")) }()
	<-entered
	written := make(chan struct{})
	go func() {
		_, _ = stdout.Write([]byte("text"))
		close(written)
	}()
	select {
	case <-written:
	case <-time.After(time.Minute):
		t.Fatal("stdout waited for a stalled stderr")
	}
}

type cancellingTerminal struct {
	noTerminal

	cancel     context.CancelFunc
	passphrase string
}

func (c cancellingTerminal) ReadSecret(string) ([]byte, error) {
	c.cancel()
	return []byte(c.passphrase), nil
}

// A Ctrl-C before the claim, here during the passphrase prompt, ends read without spending the open.
func TestACtrlCBeforeTheClaimSpendsNothing(t *testing.T) {
	t.Parallel()
	passphrase := "correct horse battery staple"
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphraseFile, []byte(passphrase+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	instance := newTestServer(t)
	text := "still unopened"
	link := createText(t, instance, text, "--passphrase-file", passphraseFile)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	environment := readEnvironment(link, io.Discard, io.Discard)
	environment.Terminal = cancellingTerminal{cancel: stop, passphrase: passphrase}
	if code := RunRead(ctx, environment, []string{"--stdout"}); code == command.ExitOK {
		t.Fatal("read went on after a Ctrl-C before the claim")
	}
	result := runWith(t, link, "read", "--stdout", "--passphrase-file", passphraseFile)
	if result.code != command.ExitOK || result.stdout != text+"\n" {
		t.Fatalf("the open was spent: exit %d, stdout %q, stderr %q", result.code, result.stdout, result.stderr)
	}
}

func TestReadHoldsAHangupOnlyWhenItCanStillDeliver(t *testing.T) {
	if signal.Ignored(syscall.SIGHUP) {
		t.Skip("the tests run with hangups ignored")
	}
	t.Parallel()
	for hangup, want := range map[bool][]os.Signal{
		true:  {syscall.SIGTERM, syscall.SIGHUP},
		false: {syscall.SIGTERM},
	} {
		if got := (&claimGuard{hangup: hangup}).heldSignals(); !slices.Equal(got, want) {
			t.Errorf("hangup %v: held %v, want %v", hangup, got, want)
		}
	}
}

func TestReadLeavesAHangupThatNohupIgnoresAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no SIGHUP")
	}
	if os.Getenv(nohupChild) != "" {
		signal.Ignore(syscall.SIGHUP)
		reportVerdict(t, fmt.Sprint((&claimGuard{hangup: true}).heldSignals()))
		return
	}
	t.Parallel()
	if verdict := childVerdict(
		t,
		"TestReadLeavesAHangupThatNohupIgnoresAlone",
		nohupChild,
		false,
	); verdict != "[terminated]" {
		t.Fatalf("held %s under nohup", verdict)
	}
}

func TestAStalledWarningDoesNotHoldUpTheEndOfRead(t *testing.T) {
	t.Parallel()
	entered, unblock := make(chan struct{}, 1), make(chan struct{})
	defer close(unblock)
	guard := &claimGuard{environment: &command.Environment{Stderr: stalledWriter{entered: entered, release: unblock}}}
	if _, err := guard.hold(t.Context(), time.Hour); err != nil {
		t.Fatal(err)
	}
	guard.interrupt <- os.Interrupt
	<-entered
	released := make(chan struct{})
	go func() {
		guard.release()
		close(released)
	}()
	select {
	case <-released:
	case <-time.After(time.Minute):
		t.Fatal("read waited for a stalled warning")
	}
}

// Once the time after a stop request has run out, a further termination request ends read at once, even though
// cli.Run, as emulated here, still listens for SIGTERM.
func TestAStopRequestAfterTheGraceEndsRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test sends itself signals")
	}
	if os.Getenv(afterGraceChild) != "" {
		ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		guard := &claimGuard{environment: &command.Environment{Stderr: io.Discard}}
		claimCtx, err := guard.hold(ctx, time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		guard.held <- syscall.SIGTERM
		<-claimCtx.Done()
		signalSelf(t, syscall.SIGTERM)
		time.Sleep(time.Minute)
		reportVerdict(t, "survived")
		return
	}
	t.Parallel()
	if child := runChild(t, "TestAStopRequestAfterTheGraceEndsRead", afterGraceChild, false); child.code != -1 {
		t.Fatalf("exit %d, verdict %q after the grace\n%s", child.code, child.verdict, child.output)
	}
}

func TestAStopRequestLeavesTheClaimOneTransferTime(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		stop os.Signal
		send func(*claimGuard, os.Signal, context.CancelCauseFunc)
	}{
		{"a termination request", syscall.SIGTERM, func(g *claimGuard, stop os.Signal, _ context.CancelCauseFunc) {
			g.held <- stop
		}},
		{"a hangup", syscall.SIGHUP, func(g *claimGuard, stop os.Signal, _ context.CancelCauseFunc) {
			g.held <- stop
		}},
		{"Ctrl-C", os.Interrupt, func(g *claimGuard, stop os.Signal, _ context.CancelCauseFunc) {
			g.interrupt <- stop
		}},
		{"a Ctrl-C that reached only cli.Run", os.Interrupt, func(_ *claimGuard, stop os.Signal, cancel context.CancelCauseFunc) {
			cancel(stopsignal.StopError{Signal: stop})
		}},
	} {
		stop := test.stop
		ctx, cancel := context.WithCancelCause(t.Context())
		pending := new(stopsignal.Pending)
		guard := &claimGuard{environment: &command.Environment{Stderr: io.Discard, Stop: pending}, hangup: true}
		claimCtx, err := guard.hold(ctx, time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-claimCtx.Done():
			t.Fatal("the claim ended before a stop request")
		default:
		}
		test.send(guard, stop, cancel)
		select {
		case <-claimCtx.Done():
		case <-time.After(time.Minute):
			t.Fatalf("%s: the claim outlived its time after a stop request", test.name)
		}
		if cause := context.Cause(claimCtx); !errors.Is(cause, errStopTimedOut) {
			t.Fatalf("%s: the claim ended with %v", test.name, cause)
		}
		guard.release()
		cancel(nil)
		if held := pending.Signal(); held != stop || !pending.Held() {
			t.Fatalf("%s: read keeps %v for the process to end by", test.name, held)
		}
	}
}

// release waits for a stop that cancelled the command just before, so the exit cannot miss it; one after release
// holds nothing.
func TestReleaseKeepsALateStopAndNothingAfterIt(t *testing.T) {
	t.Parallel()
	for range 200 {
		ctx, cancel := context.WithCancelCause(t.Context())
		pending := new(stopsignal.Pending)
		guard := &claimGuard{environment: &command.Environment{Stderr: io.Discard, Stop: pending}}
		if _, err := guard.hold(ctx, time.Hour); err != nil {
			t.Fatal(err)
		}
		cancel(stopsignal.StopError{Signal: syscall.SIGTERM})
		guard.release()
		if !pending.Held() || pending.Signal() != syscall.SIGTERM {
			t.Fatalf("release missed a stop that came just before it: %v", pending.Signal())
		}
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	pending := new(stopsignal.Pending)
	guard := &claimGuard{environment: &command.Environment{Stderr: io.Discard, Stop: pending}}
	if _, err := guard.hold(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	guard.release()
	cancel(stopsignal.StopError{Signal: syscall.SIGTERM})
	if pending.Held() {
		t.Fatal("a stop after read finished was held")
	}
}

// A stop signal that reaches only cli.Run, just before the guard registers, cancels the command after the claim has
// started; read still saves the message and the process then ends by that signal.
func TestAStopThatReachesOnlyTheCommandAfterTheClaimStillEndsRead(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	instance := serveWith(t, func(request *http.Request) {
		if strings.HasSuffix(request.URL.Path, api.ClaimAction) {
			cancel(stopsignal.StopError{Signal: os.Interrupt})
		}
	})
	text := "saved after a late stop"
	link := createText(t, instance, text)
	var stdout bytes.Buffer
	environment := readEnvironment(link, &stdout, io.Discard)
	environment.Stop = new(stopsignal.Pending)
	code := RunRead(ctx, environment, []string{"--stdout"})
	if code != command.ExitOK || stdout.String() != text+"\n" || !environment.Stop.Held() ||
		environment.Stop.Signal() != os.Interrupt {
		t.Fatalf(
			"exit %d, text %q, held %v %v",
			code,
			stdout.String(),
			environment.Stop.Held(),
			environment.Stop.Signal(),
		)
	}
}

func TestAStopBeforeTheClaimNamesTheSignal(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stopsignal.StopError{Signal: syscall.SIGTERM})
	guard := &claimGuard{environment: &command.Environment{Stderr: io.Discard}}
	if _, err := guard.hold(ctx, time.Second); err == nil || err.Error() != "terminated signal received" {
		t.Fatalf("hold refused with %v", err)
	}
}

// deliveredText is the text read printed with --stdout, or the text file it saved beside the attachment.
func deliveredText(arguments []string, stdout string, saved []string) string {
	if arguments != nil || len(saved) != 1 {
		return strings.TrimSuffix(stdout, "\n")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(saved[0]), textFileName))
	if err != nil {
		return err.Error()
	}
	return string(data)
}

// childVerdict runs one test of this binary again in a child process, which reports its verdict on descriptor 3. A
// child with closed output pipes cannot report a clean exit, so only a death by signal fails it; any other child must
// pass.
func childVerdict(t *testing.T, test, variable string, closedOutput bool) string {
	t.Helper()
	child := runChild(t, test, variable, closedOutput)
	if child.code == -1 {
		t.Fatalf("a signal ended read\n%s", child.output)
	}
	if child.code != 0 && !closedOutput {
		t.Fatalf("the child exited with %d\n%s", child.code, child.output)
	}
	return child.verdict
}

type childRun struct {
	verdict string
	code    int
	signal  syscall.Signal
	output  []byte
}

func runChild(t *testing.T, test, variable string, closedOutput bool) childRun {
	t.Helper()
	report, reportWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer report.Close()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+test+"$")
	child.Env = append(os.Environ(), variable+"=1")
	var combined bytes.Buffer
	child.Stdout, child.Stderr = &combined, &combined
	if closedOutput {
		child.Stdout, child.Stderr = closedPipe(t), closedPipe(t)
	}
	child.ExtraFiles = []*os.File{reportWriter}
	err = child.Run()
	reportWriter.Close()
	run := childRun{}
	if exit, exited := errors.AsType[*exec.ExitError](err); exited {
		run.code = exit.ExitCode()
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			run.signal = status.Signal()
		}
	} else if err != nil {
		t.Fatal(err)
	}
	reported, err := io.ReadAll(report)
	if err != nil {
		t.Fatal(err)
	}
	run.verdict, run.output = string(reported), combined.Bytes()
	return run
}

func reportVerdict(t *testing.T, verdict string) {
	t.Helper()
	if _, err := os.NewFile(3, "report").WriteString(verdict); err != nil {
		t.Fatal(err)
	}
}

func closedPipe(t *testing.T) *os.File {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	t.Cleanup(func() { writer.Close() })
	return writer
}
