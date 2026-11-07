package messages

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
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
	settings.DataDir = directory
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

func TestWritePrivateNeverOverwrites(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if _, err := writePrivate(directory, "a.txt", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := writePrivate(directory, "a.txt", []byte("two")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrote an existing file: %v", err)
	}
	if got := mustRead(t, filepath.Join(directory, "a.txt")); got != "one" {
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
	directory := t.TempDir()
	for _, name := range []string{"../escape", "sub/../../escape"} {
		if _, err := writePrivate(directory, name, []byte("x")); err == nil {
			t.Fatalf("wrote %q outside %s", name, directory)
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
	paths, err := writeAll(directory, []outputFile{{"taken.bin", []byte("new")}})
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
		locked,
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
		full,
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
	if _, err = writeAll(partial, []outputFile{{textFileName, []byte("x")}, {"a.bin", []byte("y")}}); err == nil {
		t.Fatal("writeAll succeeded with nowhere to put the attachment")
	}
	if _, err = os.Stat(filepath.Join(partial, textFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a failed set of files left the text behind")
	}
}
