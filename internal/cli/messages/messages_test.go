package messages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
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
	httpServer := httptest.NewServer(apiServer.HTTPServer().Handler)
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

func createText(t *testing.T, instance *testServer, text string) string {
	t.Helper()
	result := runWith(t, text, "create", "--server", instance.url)
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
	paths, err := writeAll(openRoot(t, directory), []outputFile{{"taken.bin", []byte("new")}})
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
	return RunRead(t.Context(), &command.Environment{
		Stdin:    strings.NewReader(link),
		Stdout:   stdout,
		Stderr:   stderr,
		Getenv:   func(string) string { return "" },
		Terminal: noTerminal{},
		Version:  "test",
	}, arguments)
}

func savedAttachments(t *testing.T, out string) []string {
	t.Helper()
	saved, err := filepath.Glob(filepath.Join(out, outputDirectoryPrefix+"*", "notes.bin"))
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

const closedPipesChild = "NYLZO_TEST_CLOSED_PIPES"

func TestReadOutlivesClosedOutputPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no SIGPIPE")
	}
	if os.Getenv(closedPipesChild) != "" {
		readIntoClosedPipes(t)
		return
	}
	t.Parallel()
	report, reportWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer report.Close()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestReadOutlivesClosedOutputPipes$")
	child.Env = append(os.Environ(), closedPipesChild+"=1")
	child.Stdout, child.Stderr = closedPipe(t), closedPipe(t)
	child.ExtraFiles = []*os.File{reportWriter}
	err = child.Run()
	reportWriter.Close()
	exit, exited := errors.AsType[*exec.ExitError](err)
	if err != nil && !exited {
		t.Fatal(err)
	}
	if exited && exit.ExitCode() == -1 {
		t.Fatalf("read died on its closed pipes: %v", err)
	}
	if reported, _ := io.ReadAll(report); string(reported) != "saved" {
		t.Fatalf("the child reported %q", reported)
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
