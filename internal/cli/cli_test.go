package cli

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/cli/messages"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/signing"

	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/store"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const reexecEnv = "NYLZO_CLI_TEST_REEXEC"

var testPepper = bytes.Repeat([]byte{7}, pepper.SecretLen)

func TestMain(tests *testing.M) {
	if os.Getenv(reexecEnv) == "1" {
		Main(
			os.Args[1:],
			&command.Environment{
				Stdin:    os.Stdin,
				Stdout:   os.Stdout,
				Stderr:   os.Stderr,
				Getenv:   os.Getenv,
				Terminal: noTerminal{},
				Harden:   noHarden,
			},
		)
	}
	tests.Run()
}

type testServer struct {
	url       string
	directory string
	requests  atomic.Int64
	hold      func(*http.Request) bool
	release   chan struct{}
}

func newTestServer(t *testing.T, change func(*config.Server)) *testServer {
	t.Helper()
	directory := t.TempDir()
	settings := config.Defaults()
	settings.DataDirectory = directory
	settings.Pepper = bytes.Clone(testPepper)
	settings.ReportKey = testReportKey.PublicKey()
	settings.Development.AllowUnerasableStorage = runtime.GOOS != "linux"
	settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 1<<20, 1<<20
	settings.Rates.SourcePerSecond, settings.Rates.SourceBurst = 1<<20, 1<<20
	settings.Rates.CreatePerHour, settings.Rates.CreatePerDay, settings.Rates.ReadPerHour = 1000, 1000, 1000
	if change != nil {
		change(&settings)
	}
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
	instance := &testServer{directory: directory, release: make(chan struct{})}
	handler := apiServer.HTTPServer().Handler
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		instance.requests.Add(1)
		if instance.hold != nil && instance.hold(r) {
			<-instance.release
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	t.Cleanup(apiServer.Close)
	instance.url = httpServer.URL
	return instance
}

type scriptedTerminal struct {
	mutex   sync.Mutex
	secrets []string
	stdout  bool
}

func (*scriptedTerminal) StdinIsTerminal() bool { return false }

func (s *scriptedTerminal) StdoutIsTerminal() bool { return s.stdout }

func (s *scriptedTerminal) ReadSecret(string) ([]byte, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(s.secrets) == 0 {
		return nil, console.ErrNoTerminal
	}
	next := s.secrets[0]
	s.secrets = s.secrets[1:]
	return []byte(next), nil
}

type commandResult struct {
	code   int
	stdout string
	stderr string
}

func runWith(
	t *testing.T,
	stdin string,
	variables map[string]string,
	terminal console.Terminal,
	arguments ...string,
) commandResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if terminal == nil {
		terminal = noTerminal{}
	}
	code := Run(
		arguments,
		&command.Environment{
			Stdin:    strings.NewReader(stdin),
			Stdout:   &stdout,
			Stderr:   &stderr,
			Getenv:   getenvFrom(variables),
			Terminal: terminal,
			Version:  "test",
			Harden:   noHarden,
		},
	).Code
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func createText(t *testing.T, instance *testServer, text string, arguments ...string) (string, commandResult) {
	t.Helper()
	result := runWith(t, text, nil, nil, append([]string{"create", "--server", instance.url}, arguments...)...)
	if result.code != command.ExitOK {
		t.Fatalf("create: exit %d, %s", result.code, result.stderr)
	}
	link, _, _ := strings.Cut(result.stdout, "\n")
	return link, result
}

func withDestination(name string) []string {
	if name == "read" {
		return []string{name, "--stdout"}
	}
	return []string{name}
}

func readPaths(t *testing.T, result commandResult) []string {
	t.Helper()
	if result.code != command.ExitOK {
		t.Fatalf("read: exit %d, %s", result.code, result.stderr)
	}
	return strings.Fields(result.stdout)
}

func assertPrivate(t *testing.T, path, want string) {
	t.Helper()
	assertPrivateIn(t, path, want, []string{path, filepath.Dir(path)})
}

func assertPrivateFile(t *testing.T, path, want string) {
	t.Helper()
	assertPrivateIn(t, path, want, []string{path})
}

func assertPrivateIn(t *testing.T, path, want string, private []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s holds %q, want %q", path, data, want)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for _, privatePath := range private {
		info, statErr := os.Stat(privatePath)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is open to other users: %v", privatePath, info.Mode().Perm())
		}
	}
}

func TestCreateAndReadText(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	manage := filepath.Join(t.TempDir(), "manage")
	link, created := createText(
		t,
		instance,
		"tajná zpráva\nna dva řádky",
		"--manage-token-file",
		manage,
		"--ttl",
		"1h",
		"--opens",
		"2",
	)
	if !strings.HasPrefix(link, instance.url+"/r#2.0.") || strings.Count(created.stdout, "\n") != 1 {
		t.Fatalf("create printed %q", created.stdout)
	}
	if strings.Contains(created.stderr, "#") {
		t.Fatalf("create wrote the link to stderr: %q", created.stderr)
	}
	paths := readPaths(t, runWith(t, link, nil, nil, "read", "--out", t.TempDir()))
	if len(paths) != 1 || filepath.Base(paths[0]) != "message.txt" {
		t.Fatalf("read wrote %v", paths)
	}
	assertPrivate(t, paths[0], "tajná zpráva\nna dva řádky")
	assertPrivateFile(t, manage, mustRead(t, manage))
	status := runWith(t, "", nil, nil, "status", "--manage-token-file", manage)
	if status.code != command.ExitOK || !strings.Contains(status.stdout, "state: available") ||
		!strings.Contains(status.stdout, "opens: 1 of 2 used") {
		t.Fatalf("status: %d %s %s", status.code, status.stdout, status.stderr)
	}
	if result := runWith(
		t,
		"",
		nil,
		nil,
		"cancel",
		"--manage-token-file",
		manage,
	); result.code != command.ExitOK ||
		result.stdout != "cancelled\n" {
		t.Fatalf("cancel: %d %q %q", result.code, result.stdout, result.stderr)
	}
	if result := runWith(
		t,
		link,
		nil,
		nil,
		"read",
		"--out",
		t.TempDir(),
	); result.code != command.ExitError ||
		!strings.Contains(result.stderr, "the sender cancelled the message") {
		t.Fatalf("read after cancel: %d %q", result.code, result.stderr)
	}
	ended := runWith(t, "", nil, nil, "status", "--manage-token-file", manage)
	if ended.code != command.ExitOK || !strings.Contains(ended.stdout, "state: cancelled\n") ||
		!strings.Contains(ended.stdout, "\nended: ") ||
		!strings.HasSuffix(ended.stdout, "\nopens and failed passphrase attempts: no longer kept\n") ||
		strings.Contains(ended.stdout, "of 0 used") {
		t.Fatalf("status after cancel: %d %q", ended.code, ended.stdout)
	}
}

func TestCreateSealsEveryMessageWithItsOwnIV(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	first, _ := createText(t, instance, "the same text")
	second, _ := createText(t, instance, "the same text")
	var ivs [][]byte
	for _, link := range []string{first, second} {
		header, err := envelope.ParseHeader(storedEnvelope(t, instance, link))
		if err != nil {
			t.Fatalf("a stored blob is not an envelope: %v", err)
		}
		ivs = append(ivs, header.IV[:])
	}
	if bytes.Equal(ivs[0], ivs[1]) {
		t.Fatalf("stored IVs %x, want two different ones", ivs)
	}
}

// storedEnvelope opens the sealed record with the link's read key and decrypts the stored blob, as the server does.
func storedEnvelope(t *testing.T, instance *testServer, link string) []byte {
	t.Helper()
	parsed, err := core.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	linkKeys, err := keys.DeriveLink(parsed.ID, &parsed.Secret)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := sqlite.Open(t.Context(), filepath.Join(instance.directory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	locator := seal.LocatorOf(parsed.ID[:])
	row, err := storage.Load(t.Context(), locator)
	if err != nil {
		t.Fatal(err)
	}
	readKey := signing.PublicKey(&linkKeys.R)
	ring, err := pepper.NewRing(testPepper, nil)
	if err != nil {
		t.Fatal(err)
	}
	recordKey, err := seal.UnwrapRecordKey(ring.RecordsKey(), readKey[:], row.ReadWrap, locator, seal.Read)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.OpenRecord(&row, recordKey)
	if err != nil {
		t.Fatal(err)
	}
	blobKey, err := record.BlobKey()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(
		filepath.Join(instance.directory, disk.Directory, hex.EncodeToString([]byte(row.BlobReference))),
	)
	if err != nil {
		t.Fatal(err)
	}
	salt, err := seal.ReadEraseBlock(bytes.NewReader(stored))
	if err != nil {
		t.Fatal(err)
	}
	encrypted := stored[erase.BlockLen:]
	object := make([]byte, record.Message.Size)
	seal.BlobStream(blobKey.Key, salt).XORKeyStream(object, encrypted[:record.Message.Size])
	return object
}

func TestALinkFromANewerVersionAsksForANewerNylzo(t *testing.T) {
	t.Parallel()
	secret := core.Encode(make([]byte, core.SecretLen))
	newer := "https://nylzo.example/r#3.3." + core.NewID().String() + "." + secret + "." + secret
	damaged := "https://nylzo.example/r#2.9." + core.NewID().String() + "." + secret + "." + secret
	for _, name := range []string{"read", "burn", "report"} {
		result := runWith(t, newer, nil, nil, withDestination(name)...)
		if result.code != command.ExitError ||
			!strings.Contains(
				result.stderr,
				"the link was made by a newer version of nylzo; update nylzo from https://github.com/misaon/nylzo/releases",
			) {
			t.Errorf("%s with a newer link: %d %q", name, result.code, result.stderr)
		}
		result = runWith(t, damaged, nil, nil, withDestination(name)...)
		if result.code != command.ExitError || !strings.Contains(result.stderr, "the link is incomplete or damaged") ||
			strings.Contains(result.stderr, "newer") {
			t.Errorf("%s with a damaged link: %d %q", name, result.code, result.stderr)
		}
	}
}

func TestRetiredAndReleasedLinksAskForANewLink(t *testing.T) {
	t.Parallel()
	secret := core.Encode(make([]byte, core.SecretLen))
	id := core.NewID().String()
	for link, want := range map[string]string{
		"https://nylzo.example/r#2.2." + id + "." + secret + "." + secret: "the link uses a passphrase profile " +
			"that nylzo no longer supports; ask the sender for a new link",
		"https://nylzo.example/m/" + id + "#1.0." + secret: "the link is from before this instance's security " +
			"upgrade, which deleted every message it held; ask the sender for a new link",
	} {
		for _, name := range []string{"read", "burn", "report"} {
			result := runWith(t, link, nil, nil, withDestination(name)...)
			if result.code != command.ExitError || !strings.Contains(result.stderr, want) {
				t.Errorf("%s with %s: %d %q", name, link, result.code, result.stderr)
			}
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPassphraseFromFilesAndWrongAttempts(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	right := writeSecretFile(t, "correct horse battery staple\n")
	wrong := writeSecretFile(t, "wrong horse battery staple")
	link, _ := createText(t, instance, "behind a passphrase", "--passphrase-file", right)
	if !strings.Contains(link, "#2.3.") {
		t.Fatalf("link %s", link)
	}
	result := runWith(t, link, nil, nil, "read", "--passphrase-file", wrong, "--out", t.TempDir())
	if result.code != command.ExitError || !strings.Contains(result.stderr, "wrong passphrase; 4 attempts left") {
		t.Fatalf("wrong passphrase: %d %q", result.code, result.stderr)
	}
	if result = runWith(t, link, nil, nil, "read", "--out", t.TempDir()); result.code != command.ExitError ||
		!strings.Contains(result.stderr, "needs a passphrase") {
		t.Fatalf("no passphrase: %d %q", result.code, result.stderr)
	}
	paths := readPaths(
		t,
		runWith(t, link, map[string]string{command.EnvironmentPassphrase: right}, nil, "read", "--out", t.TempDir()),
	)
	assertPrivate(t, paths[0], "behind a passphrase")
}

func TestBurnNeedsThePassphrase(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	right := writeSecretFile(t, "correct horse battery staple\n")
	link, _ := createText(t, instance, "burn behind a passphrase", "--passphrase-file", right)
	wrong := writeSecretFile(t, "wrong horse battery staple")
	result := runWith(t, link, nil, nil, "burn", "--passphrase-file", wrong)
	if result.code != command.ExitError || !strings.Contains(result.stderr, "wrong passphrase; 4 attempts left") {
		t.Fatalf("burn with a wrong passphrase: %d %q", result.code, result.stderr)
	}
	if result = runWith(t, link, nil, nil, "burn"); result.code != command.ExitError ||
		!strings.Contains(result.stderr, "needs a passphrase") {
		t.Fatalf("burn without a passphrase: %d %q", result.code, result.stderr)
	}
	if result = runWith(t, link, nil, nil, "burn", "--passphrase-file", right); result.code != command.ExitOK ||
		result.stdout != "deleted\n" {
		t.Fatalf("burn with the passphrase: %d %q %q", result.code, result.stdout, result.stderr)
	}
}

func TestCreateHasNoProfileChoice(t *testing.T) {
	t.Parallel()
	result := runWith(
		t,
		"secret",
		nil,
		nil,
		"create",
		"--server",
		"https://nylzo.example",
		"--passphrase-file",
		writeSecretFile(t, "correct horse battery staple"),
		"--kdf",
		"scrypt",
	)
	if result.code != command.ExitUsage || !strings.Contains(result.stderr, "unknown or incomplete flag") {
		t.Fatalf("--kdf: %d %q", result.code, result.stderr)
	}
}

func TestAskedPassphraseMustMatchAndBeStrong(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	for _, testCase := range []struct {
		secrets []string
		want    string
	}{
		{[]string{"correct horse battery staple", "correct horse battery stapel"}, "the two passphrases differ"},
		{[]string{"short", "short"}, "at least 15 characters"},
		{[]string{"twelve chars", "twelve chars"}, "at least 15 characters"},
		{[]string{"aaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaa"}, "repeats too few different characters"},
		{[]string{"Polniypizdec0211", "Polniypizdec0211"}, "commonly used passwords"},
		{[]string{"tired face \U0001FAE9 is new", "tired face \U0001FAE9 is new"}, "Unicode 15.1 does not know"},
	} {
		result := runWith(
			t,
			"text",
			nil,
			&scriptedTerminal{secrets: testCase.secrets},
			"create",
			"--server",
			instance.url,
			"--ask-passphrase",
		)
		if result.code != command.ExitError || !strings.Contains(result.stderr, testCase.want) ||
			instance.requests.Load() != 0 {
			t.Fatalf("%v: %d %q, %d requests", testCase.secrets, result.code, result.stderr, instance.requests.Load())
		}
	}
	terminal := &scriptedTerminal{secrets: []string{"fifteen chars x", "fifteen chars x"}}
	result := runWith(t, "text", nil, terminal, "create", "--server", instance.url, "--ask-passphrase")
	if result.code != command.ExitOK ||
		!strings.Contains(result.stderr, "warning: a passphrase of at least 4 words and 16 characters") {
		t.Fatalf("a weak but valid passphrase: %d %q", result.code, result.stderr)
	}
	link, _, _ := strings.Cut(result.stdout, "\n")
	paths := readPaths(
		t,
		runWith(t, link, nil, &scriptedTerminal{secrets: []string{"fifteen chars x"}}, "read", "--out", t.TempDir()),
	)
	assertPrivate(t, paths[0], "text")
}

func TestAttachmentAndStdout(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	const rightToLeftOverride = "\u202e"
	source := filepath.Join(t.TempDir(), "report"+rightToLeftOverride+"gpj.exe")
	payload := bytes.Repeat([]byte{0, 1, 2, 250}, 300_000)
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	link, _ := createText(t, instance, "", "--from-file", source)
	out := t.TempDir()
	paths := readPaths(t, runWith(t, link, nil, nil, "read", "--out", out))
	if len(paths) != 1 || filepath.Dir(filepath.Dir(paths[0])) != out || filepath.Base(paths[0]) != "reportgpj.exe" {
		t.Fatalf("attachment written to %v", paths)
	}
	if got, _ := os.ReadFile(paths[0]); !bytes.Equal(got, payload) {
		t.Fatal("the attachment differs from the file that was sent")
	}
	text, _ := createText(t, instance, "to stdout")
	if result := runWith(
		t,
		text,
		nil,
		nil,
		"read",
		"--stdout",
	); result.code != command.ExitOK ||
		result.stdout != "to stdout\n" {
		t.Fatalf("read --stdout: %d %q %q", result.code, result.stdout, result.stderr)
	}
}

func TestBurnAndPrintedManageToken(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	result := runWith(
		t,
		"burn me",
		nil,
		nil,
		"create",
		"--server",
		instance.url,
		"--opens",
		"3",
		"--print-manage-token",
	)
	lines := strings.Split(strings.TrimSpace(result.stdout), "\n")
	if result.code != command.ExitOK || len(lines) != 3 || strings.Contains(lines[1], "#") {
		t.Fatalf("create --print-manage-token: %d %q", result.code, result.stdout)
	}
	if burned := runWith(
		t,
		"",
		map[string]string{command.EnvironmentLink: lines[0]},
		nil,
		"burn",
	); burned.code != command.ExitOK ||
		burned.stdout != "deleted\n" {
		t.Fatalf("burn: %d %q %q", burned.code, burned.stdout, burned.stderr)
	}
	status := runWith(t, lines[1]+"\n"+lines[2]+"\n", nil, nil, "status", "--json")
	if status.code != command.ExitOK || !strings.Contains(status.stdout, `"state":"deleted"`) {
		t.Fatalf("status after burn: %d %q %q", status.code, status.stdout, status.stderr)
	}
}

func TestCreateSolvesProofOfWork(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, func(settings *config.Server) {
		settings.Rates.GlobalDailyCreates = 4
		settings.PoWDifficulty = 2
	})
	for i := range 3 {
		result := runWith(t, "busy server", nil, nil, "create", "--server", instance.url)
		if result.code != command.ExitOK {
			t.Fatalf("create %d: %d %q", i, result.code, result.stderr)
		}
		if i >= 2 && !strings.Contains(result.stderr, "solving a proof of work") {
			t.Fatalf("create %d did not solve a proof of work: %q", i, result.stderr)
		}
	}
}

func TestSecretsNeverTravelInArguments(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "never in argv")
	before := instance.requests.Load()
	head, fragment, _ := strings.Cut(link, "#")
	for _, arguments := range [][]string{
		{"read", link},
		{"open", link},
		{"open", head},
		{"web", "--server", link},
		{"read", "--out", link},
		{"burn", "--link-file=" + link},
		{"create", "--server", link},
		{"status", "#" + fragment},
		{"cancel", head + "#" + fragment},
		{"create", "--server", instance.url, "my secret text"},
		{"read", head},
		{"status", core.Encode(make([]byte, 32))},
		{"create", "--server", instance.url, "--ttl", "1h", "correct horse battery staple"},
	} {
		result := runWith(t, "", nil, nil, arguments...)
		if result.code != command.ExitUsage || strings.Contains(result.stderr, fragment) {
			t.Fatalf("%v: exit %d, stderr %q", arguments, result.code, result.stderr)
		}
	}
	if instance.requests.Load() != before {
		t.Fatal("a refused command still talked to the server")
	}
}

var flagLine = regexp.MustCompile(`(?m)^ {2}-([a-z-]+)`)

func TestNoFlagTakesASecret(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"server":                   true,
		"from-file":                true,
		"ttl":                      true,
		"opens":                    true,
		"passphrase-file":          true,
		"ask-passphrase":           true,
		"generate-passphrase-file": true,
		"manage-token-file":        true,
		"print-manage-token":       true,
		"compress":                 true,
		"link-file":                true,
		"out":                      true,
		"stdout":                   true,
		"json":                     true,
		"allow-header":             true,
		"reason-file":              true,
		"contact":                  true,
		"good-faith":               true,
		"key-file":                 true,
		"no-browser":               true,
	}
	for _, subcommand := range commands() {
		result := runWith(t, "", nil, nil, subcommand.name, "-h")
		if result.code != command.ExitOK || result.stderr != "" {
			t.Fatalf("%s -h: exit %d, stderr %q", subcommand.name, result.code, result.stderr)
		}
		for _, match := range flagLine.FindAllStringSubmatch(result.stdout, -1) {
			if !allowed[match[1]] {
				t.Fatalf(
					"%s has the flag -%s, which is not on the list of flags known to carry no secret",
					subcommand.name,
					match[1],
				)
			}
		}
	}
}

func TestProcessListShowsNoSecretSmoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ps on Windows")
	}
	processList, err := exec.LookPath("ps")
	if err != nil {
		t.Skip("no ps")
	}
	t.Parallel()
	instance := newTestServer(t, nil)
	passphrase := writeSecretFile(t, "correct horse battery staple")
	link, _ := createText(t, instance, "watched by ps", "--passphrase-file", passphrase)
	held := make(chan struct{})
	var once sync.Once
	instance.hold = func(r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/claim") {
			once.Do(func() { close(held) })
			return true
		}
		return false
	}
	process := exec.Command(os.Args[0], "read", "--passphrase-file", passphrase, "--out", t.TempDir())
	process.Env = append(os.Environ(), reexecEnv+"=1")
	process.Stdin = strings.NewReader(link)
	var out bytes.Buffer
	process.Stdout, process.Stderr = &out, &out
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(30 * time.Second):
		t.Fatal("the claim never arrived")
	}
	listing, err := exec.Command(processList, "-o", "args=", "-p", strconv.Itoa(process.Process.Pid)).Output()
	close(instance.release)
	waitErr := process.Wait()
	if err != nil {
		t.Fatal(err)
	}
	_, fragment, _ := strings.Cut(link, "#")
	if !strings.Contains(string(listing), "read") || strings.Contains(string(listing), fragment) ||
		strings.Contains(string(listing), "correct horse") {
		t.Fatalf("ps shows %q", listing)
	}
	if waitErr != nil {
		t.Fatalf("read: %v %s", waitErr, out.String())
	}
}

type breakingWriter struct {
	http.ResponseWriter

	limit int
}

func (b *breakingWriter) Write(data []byte) (int, error) {
	if len(data) > b.limit {
		data = data[:b.limit]
	}
	count, _ := b.ResponseWriter.Write(data)
	b.ResponseWriter.(http.Flusher).Flush()
	b.limit -= count
	if b.limit <= 0 {
		panic(http.ErrAbortHandler)
	}
	return count, nil
}

func TestBrokenClaimResumesThroughTheLease(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	manage := filepath.Join(t.TempDir(), "manage")
	link, _ := createText(t, instance, strings.Repeat("resume ", 2000), "--manage-token-file", manage, "--opens", "2")
	var broke atomic.Bool
	srvURL := instance.url
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := srvURL + r.URL.RequestURI()
		request, _ := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
		request.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		maps.Copy(w.Header(), response.Header)
		w.WriteHeader(response.StatusCode)
		w.(http.Flusher).Flush()
		if strings.HasSuffix(r.URL.Path, "/claim") && broke.CompareAndSwap(false, true) {
			io.Copy(&breakingWriter{ResponseWriter: w, limit: 100}, response.Body)
			return
		}
		io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	viaProxy := strings.Replace(link, instance.url, proxy.URL, 1)
	paths := readPaths(t, runWith(t, viaProxy, nil, nil, "read", "--out", t.TempDir()))
	assertPrivate(t, paths[0], strings.Repeat("resume ", 2000))
	if !broke.Load() {
		t.Fatal("the claim was never broken")
	}
	status := runWith(t, "", nil, nil, "status", "--manage-token-file", manage)
	if !strings.Contains(status.stdout, "opens: 1 of 2 used") {
		t.Fatalf("the broken claim spent another open: %s", status.stdout)
	}
}

func TestServerAddressRules(t *testing.T) {
	t.Parallel()
	for _, address := range []string{"http://nylzo.example", "https://nylzo.example/path", "https://user@nylzo.example", "https://nylzo.example/?q=1", "ftp://nylzo.example"} {
		result := runWith(t, "text", nil, nil, "create", "--server", address)
		if result.code != command.ExitError {
			t.Fatalf("%s: exit %d %q", address, result.code, result.stderr)
		}
	}
}

func TestRedirectsAreRefused(t *testing.T) {
	t.Parallel()
	target := newTestServer(t, nil)
	redirect := httptest.NewServer(http.RedirectHandler(target.url+"/api/v1/instance", http.StatusFound))
	t.Cleanup(redirect.Close)
	result := runWith(t, "text", nil, nil, "create", "--server", redirect.URL)
	if result.code != command.ExitError || !strings.Contains(result.stderr, "redirect") || target.requests.Load() != 0 {
		t.Fatalf("a redirect was followed: %d %q, %d requests", result.code, result.stderr, target.requests.Load())
	}
}

func rewritingProxy(t *testing.T, target string, rewrite func(*http.Request, []byte) []byte) string {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, _ := http.NewRequestWithContext(r.Context(), r.Method, target+r.URL.RequestURI(), r.Body)
		request.Header = r.Header.Clone()
		request.ContentLength = r.ContentLength
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		body = rewrite(r, body)
		maps.Copy(w.Header(), response.Header)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(response.StatusCode)
		w.Write(body)
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL
}

func TestLiedMetadataIsRefused(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	passphrase := writeSecretFile(t, "correct horse battery staple")
	for name, testCase := range map[string]struct {
		arguments []string
		rewrite   func(string) string
		want      string
	}{
		"opens_max":   {nil, func(body string) string { return strings.Replace(body, `"opens_max":2`, `"opens_max":3`, 1) }, "different policy"},
		"ttl_s":       {nil, func(body string) string { return strings.Replace(body, `"ttl_s":3600`, `"ttl_s":7200`, 1) }, "different policy"},
		"kdf profile": {[]string{"--passphrase-file", passphrase}, func(body string) string { return strings.Replace(body, `"kdf_id":3`, `"kdf_id":0`, 1) }, "does not match"},
		"state":       {nil, func(body string) string { return strings.Replace(body, `"state":"available"`, `"state":"opened"`, 1) }, "already opened"},
	} {
		manage := filepath.Join(t.TempDir(), "manage")
		link, _ := createText(
			t,
			instance,
			"policy",
			append([]string{"--opens", "2", "--ttl", "1h", "--manage-token-file", manage}, testCase.arguments...)...,
		)
		proxy := rewritingProxy(t, instance.url, func(r *http.Request, body []byte) []byte {
			if r.Method == http.MethodGet && strings.Count(r.URL.Path, "/") == 4 {
				return []byte(testCase.rewrite(string(body)))
			}
			return body
		})
		result := runWith(
			t,
			strings.Replace(link, instance.url, proxy, 1),
			map[string]string{command.EnvironmentPassphrase: passphrase},
			nil,
			"read",
			"--out",
			t.TempDir(),
		)
		if result.code != command.ExitError || !strings.Contains(result.stderr, testCase.want) {
			t.Fatalf("%s: %d %q", name, result.code, result.stderr)
		}
		status := runWith(t, "", nil, nil, "status", "--manage-token-file", manage)
		if name == "kdf profile" && !strings.Contains(status.stdout, "opens: 0 of 2 used") {
			t.Fatalf("a profile mismatch spent an open: %s", status.stdout)
		}
	}
}

func TestAServerCannotSwitchOffThePassphrase(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	proxy := rewritingProxy(t, instance.url, func(r *http.Request, body []byte) []byte {
		if r.URL.Path == "/api/v1/instance" {
			return []byte(strings.Replace(string(body), `"max_opens":`, `"default_kdf_id":0,"max_opens":`, 1))
		}
		return body
	})
	result := runWith(
		t,
		"secret",
		nil,
		nil,
		"create",
		"--server",
		proxy,
		"--passphrase-file",
		writeSecretFile(t, "correct horse battery staple"),
	)
	if result.code != command.ExitOK || !strings.Contains(result.stdout, "#2.3.") {
		t.Fatalf("a passphrase was dropped on the server's word: %d %q %q", result.code, result.stdout, result.stderr)
	}
}

func TestReadChecksTheOutputBeforeItSpendsAnOpen(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "still there")
	before := instance.requests.Load()
	result := runWith(t, link, nil, nil, "read", "--out", filepath.Join(t.TempDir(), "missing"))
	if result.code != command.ExitError || instance.requests.Load() != before {
		t.Fatalf("a missing --out: exit %d, %d requests", result.code, instance.requests.Load()-before)
	}
	out := t.TempDir()
	if result = runWith(t, link, nil, nil, "read", "--stdout", "--out", out); result.code != command.ExitOK ||
		result.stdout != "still there\n" {
		t.Fatalf("read --stdout: %d %q %q", result.code, result.stdout, result.stderr)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("read --stdout left %d empty directories behind", len(entries))
	}
}

func TestLiedPolicyHeaderIsRefused(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	for name, lie := range map[string]func(string) string{
		"kdf":   func(policy string) string { return strings.Replace(policy, `"kdf_id":0`, `"kdf_id":3`, 1) },
		"flags": func(policy string) string { return strings.Replace(policy, `"flags":2`, `"flags":3`, 1) },
		"deadline": func(policy string) string {
			return regexp.MustCompile(`"deadline":(\d+)`).ReplaceAllStringFunc(policy, func(field string) string {
				deadline, _ := strconv.ParseInt(strings.TrimPrefix(field, `"deadline":`), 10, 64)
				return `"deadline":` + strconv.FormatInt(deadline+60, 10)
			})
		},
	} {
		link, _ := createText(t, instance, "policy header")
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request, _ := http.NewRequestWithContext(r.Context(), r.Method, instance.url+r.URL.RequestURI(), r.Body)
			request.Header = r.Header.Clone()
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer response.Body.Close()
			maps.Copy(w.Header(), response.Header)
			if policy := response.Header.Get("X-Nylzo-Policy"); policy != "" {
				raw, _ := base64.RawURLEncoding.DecodeString(policy)
				w.Header().
					Set("X-Nylzo-Policy", core.Encode([]byte(lie(string(raw)))))
			}
			w.WriteHeader(response.StatusCode)
			io.Copy(w, response.Body)
		}))
		result := runWith(t, strings.Replace(link, instance.url, proxy.URL, 1), nil, nil, "read", "--out", t.TempDir())
		proxy.Close()
		if result.code != command.ExitError || !strings.Contains(result.stderr, "different policy") {
			t.Fatalf("%s: %d %q", name, result.code, result.stderr)
		}
	}
}

func TestCreateRetriesWithANewIDAndSignedRequestsRetryA404(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	var creates, metadata atomic.Int32
	var ids sync.Map
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/messages" {
			ids.Store(r.Header.Get("X-Nylzo-Create"), true)
			if refusal, refused := map[int32]string{1: "conflict", 2: "share_lost"}[creates.Add(1)]; refused {
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"error":"` + refusal + `"}`))
				return
			}
		}
		if r.Method == http.MethodGet && strings.Count(r.URL.Path, "/") == 4 && metadata.Add(1) == 1 {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not_found"}`))
			return
		}
		request, _ := http.NewRequestWithContext(r.Context(), r.Method, instance.url+r.URL.RequestURI(), r.Body)
		request.Header = r.Header.Clone()
		request.ContentLength = r.ContentLength
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		maps.Copy(w.Header(), response.Header)
		w.WriteHeader(response.StatusCode)
		io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	result := runWith(t, "second try", nil, nil, "create", "--server", proxy.URL)
	if result.code != command.ExitOK || creates.Load() != 3 {
		t.Fatalf(
			"create after a conflict and a lost share: %d %q, %d creates",
			result.code,
			result.stderr,
			creates.Load(),
		)
	}
	distinct := 0
	ids.Range(func(any, any) bool { distinct++; return true })
	if distinct != 3 {
		t.Fatal("the retry reused the refused create header")
	}
	link, _, _ := strings.Cut(result.stdout, "\n")
	paths := readPaths(t, runWith(t, link, nil, nil, "read", "--out", t.TempDir()))
	assertPrivate(t, paths[0], "second try")
	if metadata.Load() != 2 {
		t.Fatalf("metadata was asked %d times, want one retry after the 404", metadata.Load())
	}
}

// F7, F05: the headers of a create, a claim and a burn have one length whether or not the message has a passphrase.
func TestRequestHeadersDoNotShowAPassphrase(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	var mutex sync.Mutex
	lengths := map[string][]int{}
	instance.hold = func(r *http.Request) bool {
		var route string
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/messages":
			route = "create"
		case strings.HasSuffix(r.URL.Path, "/claim"):
			route = "claim"
		case strings.HasSuffix(r.URL.Path, "/burn"):
			route = "burn"
		default:
			return false
		}
		if route == "claim" && (r.Header.Get("X-Nylzo-Verifier") == "" || r.Header.Get("X-Nylzo-Share-Key") == "") {
			t.Errorf("a %s without a verifier or a share key", route)
		}
		if route == "burn" && r.Header.Get("X-Nylzo-Verifier") == "" {
			t.Error("a burn without a verifier")
		}
		size := 0
		for name, values := range r.Header {
			if name != "Content-Length" && name != "Authorization" {
				size += len(name) + len(strings.Join(values, ","))
			}
		}
		mutex.Lock()
		lengths[route] = append(lengths[route], size)
		mutex.Unlock()
		return false
	}
	manage := filepath.Join(t.TempDir(), "manage")
	plain, _ := createText(t, instance, "same length", "--manage-token-file", manage)
	guarded, _ := createText(
		t,
		instance,
		"same length",
		"--passphrase-file",
		writeSecretFile(t, "correct horse battery staple"),
	)
	readPaths(t, runWith(t, "\n  "+plain+"  \n", nil, nil, "read", "--out", t.TempDir()))
	passphrase := map[string]string{command.EnvironmentPassphrase: writeSecretFile(t, "correct horse battery staple")}
	readPaths(t, runWith(t, guarded, passphrase, nil, "read", "--out", t.TempDir()))
	plainBurn, _ := createText(t, instance, "same length")
	guardedBurn, _ := createText(
		t,
		instance,
		"same length",
		"--passphrase-file",
		passphrase[command.EnvironmentPassphrase],
	)
	for _, burned := range []commandResult{
		runWith(t, plainBurn, nil, nil, "burn"),
		runWith(t, guardedBurn, passphrase, nil, "burn"),
	} {
		if burned.code != command.ExitOK {
			t.Fatalf("burn: %d %q", burned.code, burned.stderr)
		}
	}
	for route, want := range map[string]int{"create": 4, "claim": 2, "burn": 2} {
		sizes := lengths[route]
		if len(sizes) != want || slices.Min(sizes) != slices.Max(sizes) {
			t.Errorf("%s headers of %v bytes; a passphrase must not change them", route, sizes)
		}
	}
	status := runWith(t, "", nil, nil, "status", "--json", "--manage-token-file", manage)
	if !regexp.MustCompile(`"ended_at":"[^"]+"`).MatchString(status.stdout) {
		t.Fatalf("the read was not acked, so the message did not end at once: %s", status.stdout)
	}
}

func TestErrorsNeverEchoSecrets(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "quiet")
	_, fragment, _ := strings.Cut(link, "#")
	secret := "correct horse battery staple"
	token := core.Encode(make([]byte, 32))
	for name, testCase := range map[string]struct {
		variables map[string]string
		arguments []string
	}{
		"passphrase in the file variable": {map[string]string{command.EnvironmentPassphrase: secret}, []string{"read", "--stdout"}},
		"token in the file variable":      {map[string]string{command.EnvironmentManageToken: token}, []string{"status"}},
		"passphrase as a file":            {nil, []string{"read", "--stdout", "--passphrase-file", secret}},
		"passphrase as a duration":        {nil, []string{"create", "--server", instance.url, "--ttl", secret}},
		"token as a count":                {nil, []string{"create", "--server", instance.url, "--opens", token}},
		"link with trailing punctuation":  {nil, []string{"read", "--stdout", "--link-file", link + ")"}},
		"link with a carriage return":     {nil, []string{"burn", "--link-file=" + link + "\r"}},
		"passphrase as --out":             {nil, []string{"read", "--out", filepath.Join(t.TempDir(), secret)}},
		"token as the manage file":        {nil, []string{"create", "--server", instance.url, "--manage-token-file", existing(t, token)}},
		"token as a command":              {nil, []string{token}},
		"passphrase as a command":         {nil, []string{secret}},
	} {
		stdin := link
		if testCase.arguments[0] == "status" {
			stdin = ""
		}
		if name == "passphrase in the file variable" || name == "passphrase as a file" {
			guarded, _ := createText(t, instance, "guarded", "--passphrase-file", writeSecretFile(t, secret))
			stdin = guarded
		}
		result := runWith(t, stdin, testCase.variables, nil, testCase.arguments...)
		if result.code == command.ExitOK || strings.Contains(result.stderr, secret) ||
			strings.Contains(result.stderr, token) ||
			strings.Contains(result.stderr, fragment) {
			t.Fatalf("%s: exit %d, stderr %q", name, result.code, result.stderr)
		}
		if strings.HasPrefix(name, "link with") && result.code != command.ExitUsage {
			t.Fatalf("%s: the link in an argument was not refused up front: exit %d", name, result.code)
		}
	}
}

func TestPassphraseFileUsesItsFirstLineAndVariablesAreNamed(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	file := writeSecretFile(t, "correct horse battery staple\r\nnote for Bob\n")
	link, _ := createText(t, instance, "first line", "--passphrase-file", file)
	result := runWith(
		t,
		"",
		map[string]string{
			command.EnvironmentLink:       link,
			command.EnvironmentPassphrase: writeSecretFile(t, "correct horse battery staple"),
		},
		nil,
		"read",
		"--out",
		t.TempDir(),
	)
	paths := readPaths(t, result)
	assertPrivate(t, paths[0], "first line")
	if !strings.Contains(result.stderr, "named by "+command.EnvironmentLink) ||
		!strings.Contains(result.stderr, "named by "+command.EnvironmentPassphrase) {
		t.Fatalf("read did not say it used the variables: %q", result.stderr)
	}
}

func TestStdoutKeepsControlCharactersOffATerminal(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "safe\x1b]0;pwned\x07text")
	result := runWith(t, link, nil, &scriptedTerminal{stdout: true}, "read", "--stdout")
	if result.code != command.ExitOK || result.stdout != "safe\\u001b]0;pwned\\u0007text\n" ||
		!strings.Contains(result.stderr, "control characters") {
		t.Fatalf("read --stdout to a terminal: %d %q %q", result.code, result.stdout, result.stderr)
	}
}

func TestReadWritesToDiskOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "stays")
	source := filepath.Join(t.TempDir(), "notes.bin")
	if err := os.WriteFile(source, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	attachment, _ := createText(t, instance, "", "--from-file", source, "--opens", "2")
	before := instance.requests.Load()
	if result := runWith(t, link, nil, nil, "read"); result.code != command.ExitUsage ||
		instance.requests.Load() != before || !strings.Contains(result.stderr, "--stdout") {
		t.Fatalf(
			"read without a destination: %d %q, %d requests",
			result.code,
			result.stderr,
			instance.requests.Load()-before,
		)
	}
	if result := runWith(t, attachment, nil, nil, "read", "--stdout"); result.code != command.ExitError ||
		!strings.Contains(result.stderr, "--out DIR") {
		t.Fatalf("read --stdout of an attachment did not say why it saved nothing: %d %q", result.code, result.stderr)
	}
	result := runWith(t, attachment, nil, nil, "read", "--out", t.TempDir())
	paths := readPaths(t, result)
	if len(paths) != 1 || mustRead(t, paths[0]) != "\x01\x02\x03" {
		t.Fatalf("the second open lost the file: %v", paths)
	}
	if !strings.Contains(result.stderr, "cannot scan attachments") {
		t.Fatalf("a saved attachment came without the warning: %q", result.stderr)
	}
	if result = runWith(t, link, nil, nil, "read", "--out", t.TempDir()); result.code != command.ExitOK ||
		strings.Contains(result.stderr, "cannot scan attachments") {
		t.Fatalf("a text-only read: %d %q", result.code, result.stderr)
	}
}

func TestGeneratedPassphraseFile(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	path := filepath.Join(t.TempDir(), "passphrase")
	link, result := createText(t, instance, "guarded", "--generate-passphrase-file", path)
	if strings.Contains(result.stderr, "warning") {
		t.Fatalf("a generated passphrase drew a warning: %q", result.stderr)
	}
	passphrase := strings.TrimSpace(mustRead(t, path))
	if err := keys.CheckPassphraseStrength(passphrase); err != nil || strings.Contains(result.stdout, passphrase) {
		t.Fatalf("generated passphrase %q: %v, stdout %q", passphrase, err, result.stdout)
	}
	assertPrivateFile(t, path, passphrase+"\n")
	if read := runWith(t, link, nil, nil, "read", "--stdout", "--passphrase-file", path); read.stdout != "guarded\n" {
		t.Fatalf("read with the generated passphrase: %d %q", read.code, read.stderr)
	}
	again := runWith(t, "x", nil, nil, "create", "--server", instance.url, "--generate-passphrase-file", path)
	if again.code != command.ExitError || !strings.Contains(again.stderr, "already exists") {
		t.Fatalf("an existing passphrase file was replaced: %d %q", again.code, again.stderr)
	}
}

func existing(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServerTextNeverReachesTheTerminalRaw(t *testing.T) {
	t.Parallel()
	hostile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte(`{"error":"\u001b]0;pwned\u0007 https://evil.example/m/x#1.0.fake"}`))
	}))
	t.Cleanup(hostile.Close)
	result := runWith(t, "text", nil, nil, "create", "--server", hostile.URL)
	if result.code != command.ExitError || strings.ContainsAny(result.stderr, "\x1b\x07") ||
		strings.Contains(result.stderr, "evil.example") {
		t.Fatalf("server text on stderr: %d %q", result.code, result.stderr)
	}
	cert := &x509.Certificate{DNSNames: []string{"\x1b]0;pwned\x07", "\x1b[2K\rnylzo: the message was saved"}}
	if explained := command.Explain(
		&url.Error{
			Op:  "Get",
			URL: "https://localhost/",
			Err: &tls.CertificateVerificationError{Err: x509.HostnameError{Certificate: cert, Host: "localhost"}},
		},
	); strings.ContainsAny(
		explained,
		"\x1b\x07\r",
	) {
		t.Fatalf("certificate names reach the terminal: %q", explained)
	}
}

func TestControlCharactersAndTheTerminalGate(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	bidi, _ := createText(t, instance, "looks\u202esafe")
	if result := runWith(
		t,
		bidi,
		nil,
		&scriptedTerminal{stdout: true},
		"read",
		"--stdout",
	); strings.Contains(result.stdout, "\u202e") ||
		!strings.Contains(result.stderr, "control characters") {
		t.Fatalf("a format character went to the terminal: %q %q", result.stdout, result.stderr)
	}
	piped, _ := createText(t, instance, "to\x1ba pipe")
	if result := runWith(t, piped, nil, nil, "read", "--stdout"); result.stdout != "to\x1ba pipe\n" {
		t.Fatalf("text for a pipe was changed: %q %q", result.stdout, result.stderr)
	}
}

func TestManageTokenVariableIsNamed(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	manage := filepath.Join(t.TempDir(), "manage")
	createText(t, instance, "named", "--manage-token-file", manage)
	result := runWith(t, "", map[string]string{command.EnvironmentManageToken: manage}, nil, "status")
	if result.code != command.ExitOK || !strings.Contains(result.stderr, "named by "+command.EnvironmentManageToken) {
		t.Fatalf("status from the variable: %d %q", result.code, result.stderr)
	}
}

func TestASecondSignalStopsTheCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals")
	}
	t.Parallel()
	for _, stop := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		stdin, keep, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		process := exec.Command(os.Args[0], "burn")
		process.Env = append(os.Environ(), reexecEnv+"=1")
		process.Stdin = stdin
		if err = process.Start(); err != nil {
			t.Fatal(err)
		}
		stdin.Close()
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		time.Sleep(300 * time.Millisecond)
		process.Process.Signal(stop)
		time.Sleep(300 * time.Millisecond)
		process.Process.Signal(stop)
		select {
		case err = <-done:
		case <-time.After(10 * time.Second):
			process.Process.Kill()
			t.Fatalf("two %v signals did not stop a command blocked on stdin", stop)
		}
		keep.Close()
		if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != -1 ||
			exit.Sys().(syscall.WaitStatus).Signal() != stop {
			t.Fatalf("the second %v did not end the command by itself: %v", stop, err)
		}
	}
}

func TestAnUnwritableOutDirectoryIsNotNamed(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "kept")
	locked := filepath.Join(t.TempDir(), "correct horse battery staple")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	before := instance.requests.Load()
	result := runWith(t, link, nil, nil, "read", "--out", locked)
	if result.code != command.ExitError || strings.Contains(result.stderr, "correct horse") ||
		instance.requests.Load() != before {
		t.Fatalf("read into an unwritable --out: %d %q, %d requests",
			result.code, result.stderr, instance.requests.Load()-before)
	}
	paths := readPaths(t, runWith(t, link, nil, nil, "read", "--out", t.TempDir()))
	assertPrivate(t, paths[0], "kept")
}

func TestRunAnswersUsageErrorsWithoutRunning(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent")
	if result := runWith(t, "", nil, nil, "serve", "--help"); result.code != 0 || result.stderr != "" ||
		!strings.HasPrefix(result.stdout, "usage: nylzo serve") {
		t.Fatalf("serve --help: exit %d, stdout %q, stderr %q", result.code, result.stdout, result.stderr)
	}
	for _, arguments := range [][]string{nil, {"sweep-once", "--dry-run"}, {"serve", "extra"}, {"bogus"}, {"version", "extra"}} {
		variables := map[string]string{
			"NYLZO_DATA_DIR":    directory,
			"NYLZO_PEPPER_FILE": filepath.Join(directory, "pepper"),
		}
		result := runWith(t, "", variables, nil, arguments...)
		if result.code != 2 || result.stdout != "" || !strings.Contains(result.stderr, "nylzo") {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", arguments, result.code, result.stdout, result.stderr)
		}
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a usage error still ran a command")
	}
	for _, arguments := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		if result := runWith(t, "", nil, nil, arguments...); result.code != 0 ||
			!strings.HasPrefix(result.stdout, "usage: nylzo ") || result.stderr != "" {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", arguments, result.code, result.stdout, result.stderr)
		}
	}
	if result := runWith(t, "", nil, nil, "version"); result.code != 0 || strings.TrimSpace(result.stdout) == "" {
		t.Fatalf("version: exit %d, %q", result.code, result.stdout)
	}
}

func TestUsageAlignsEverySummary(t *testing.T) {
	names := []string{"help"}
	for _, subcommand := range commands() {
		names = append(names, subcommand.name)
	}
	columns := map[int][]string{}
	for line := range strings.SplitSeq(usage(), "\n") {
		for _, name := range names {
			if rest, ok := strings.CutPrefix(line, "  "+name+" "); ok {
				column := len(line) - len(strings.TrimLeft(rest, " "))
				columns[column] = append(columns[column], name)
			}
		}
	}
	if len(columns) != 1 {
		t.Fatalf("the summaries start at different columns: %v", columns)
	}
	for _, listed := range columns {
		if len(listed) != len(names) {
			t.Fatalf("usage lists %v, want %v", listed, names)
		}
	}
}

func TestOpenRefusesALinkItCannotHandOver(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "opened elsewhere")
	before := instance.requests.Load()
	for name, testCase := range map[string]struct {
		stdin  string
		server string
		want   string
	}{
		"another instance": {link, "https://nylzo.example", "belongs to another instance"},
		"damaged link":     {strings.Replace(link, "#2.", "#2.9", 1), instance.url, "incomplete or damaged"},
		"newer link":       {strings.Replace(link, "#2.", "#7.", 1), instance.url, "newer version"},
		"no link":          {"", instance.url, "empty"},
	} {
		result := runWith(t, testCase.stdin, nil, nil, "open", "--no-browser", "--server", testCase.server)
		if result.code != command.ExitError || !strings.Contains(result.stderr, testCase.want) || result.stdout != "" {
			t.Fatalf("%s: %+v", name, result)
		}
	}
	for _, name := range []string{"web", "open"} {
		if result := runWith(
			t,
			link,
			nil,
			nil,
			name,
			"--no-browser",
		); result.code != command.ExitUsage ||
			result.stdout != "" {
			t.Fatalf("%s without an instance: %+v", name, result)
		}
	}
	if instance.requests.Load() != before {
		t.Fatal("a refused open talked to the instance")
	}
}

func TestOpenHandsOverAValidLinkOfItsInstance(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "handed over")
	handed, err := messages.HandOver(link, instance.url)
	if err != nil {
		t.Fatal(err)
	}
	head, fragment, _ := strings.Cut(link, "#")
	if !strings.HasSuffix(head, handed.Path) || handed.Fragment != fragment {
		t.Fatalf("%+v from %s", handed, link)
	}
}
