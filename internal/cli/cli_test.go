package cli

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/console"

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const reexecEnv = "NYLZO_CLI_TEST_REEXEC"

func TestMain(tests *testing.M) {
	if os.Getenv(reexecEnv) == "1" {
		os.Exit(
			Run(
				os.Args[1:],
				&Environment{
					Stdin:    os.Stdin,
					Stdout:   os.Stdout,
					Stderr:   os.Stderr,
					Getenv:   os.Getenv,
					Terminal: noTerminal{},
				},
			),
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
	settings.DataDir = directory
	settings.Pepper = bytes.Repeat([]byte{7}, 32)
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
	handler := apiServer.Handler()
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
		&Environment{
			Stdin:    strings.NewReader(stdin),
			Stdout:   &stdout,
			Stderr:   &stderr,
			Getenv:   getenvFrom(variables),
			Terminal: terminal,
			Version:  "test",
			TempDir:  t.TempDir(),
		},
	)
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
	if result.code != exitOK {
		t.Fatalf("create: exit %d, %s", result.code, result.stderr)
	}
	link, _, _ := strings.Cut(result.stdout, "\n")
	return link, result
}

func readPaths(t *testing.T, result commandResult) []string {
	t.Helper()
	if result.code != exitOK {
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
	if !strings.HasPrefix(link, instance.url+"/m/") || strings.Count(created.stdout, "\n") != 1 {
		t.Fatalf("create printed %q", created.stdout)
	}
	if strings.Contains(created.stderr, "#") {
		t.Fatalf("create wrote the link to stderr: %q", created.stderr)
	}
	paths := readPaths(t, runWith(t, link, nil, nil, "read"))
	if len(paths) != 1 || filepath.Base(paths[0]) != textFileName {
		t.Fatalf("read wrote %v", paths)
	}
	assertPrivate(t, paths[0], "tajná zpráva\nna dva řádky")
	assertPrivateFile(t, manage, mustRead(t, manage))
	status := runWith(t, "", nil, nil, "status", "--manage-token-file", manage)
	if status.code != exitOK || !strings.Contains(status.stdout, "state: available") ||
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
	); result.code != exitOK ||
		result.stdout != "cancelled\n" {
		t.Fatalf("cancel: %d %q %q", result.code, result.stdout, result.stderr)
	}
	if result := runWith(
		t,
		link,
		nil,
		nil,
		"read",
	); result.code != exitError ||
		!strings.Contains(result.stderr, "the sender cancelled the message") {
		t.Fatalf("read after cancel: %d %q", result.code, result.stderr)
	}
	ended := runWith(t, "", nil, nil, "status", "--manage-token-file", manage)
	if ended.code != exitOK || !strings.Contains(ended.stdout, "state: cancelled\n") ||
		!strings.Contains(ended.stdout, "\nended: ") ||
		!strings.HasSuffix(ended.stdout, "\nopens and failed passphrase attempts: no longer kept\n") ||
		strings.Contains(ended.stdout, "of 0 used") {
		t.Fatalf("status after cancel: %d %q", ended.code, ended.stdout)
	}
}

func TestCreateSealsEveryMessageWithItsOwnIV(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	createText(t, instance, "the same text")
	createText(t, instance, "the same text")
	entries, err := os.ReadDir(filepath.Join(instance.directory, disk.Directory))
	if err != nil {
		t.Fatal(err)
	}
	var ivs [][]byte
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		object, readErr := os.ReadFile(filepath.Join(instance.directory, disk.Directory, entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		header, parseErr := core.ParseHeader(object)
		if parseErr != nil {
			t.Fatalf("a stored blob is not an envelope: %v", parseErr)
		}
		ivs = append(ivs, header.IV[:])
	}
	if len(ivs) != 2 || bytes.Equal(ivs[0], ivs[1]) {
		t.Fatalf("stored IVs %x, want two different ones", ivs)
	}
}

func TestALinkFromANewerVersionAsksForANewerNylzo(t *testing.T) {
	t.Parallel()
	secret := core.Encode(make([]byte, core.SecretLen))
	newer := "https://nylzo.example/m/" + core.NewID().String() + "#2.1." + secret
	damaged := "https://nylzo.example/m/" + core.NewID().String() + "#1.9." + secret
	for _, command := range []string{"read", "burn", "report"} {
		result := runWith(t, newer, nil, nil, command)
		if result.code != exitError ||
			!strings.Contains(
				result.stderr,
				"the link was made by a newer version of nylzo; update nylzo from "+releasesURL,
			) {
			t.Errorf("%s with a newer link: %d %q", command, result.code, result.stderr)
		}
		result = runWith(t, damaged, nil, nil, command)
		if result.code != exitError || !strings.Contains(result.stderr, "the link is incomplete or damaged") ||
			strings.Contains(result.stderr, "newer") {
			t.Errorf("%s with a damaged link: %d %q", command, result.code, result.stderr)
		}
	}
}

func TestALinkWithTheRetiredProfileAsksForANewLink(t *testing.T) {
	t.Parallel()
	secret := core.Encode(make([]byte, core.SecretLen))
	retired := "https://nylzo.example/m/" + core.NewID().String() + "#1.2." + secret
	for _, command := range []string{"read", "burn", "report"} {
		result := runWith(t, retired, nil, nil, command)
		if result.code != exitError ||
			!strings.Contains(
				result.stderr,
				"the link uses a passphrase profile that nylzo no longer supports; ask the sender for a new link",
			) {
			t.Errorf("%s with kdf_id 2: %d %q", command, result.code, result.stderr)
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
	if !strings.Contains(link, "#1.1.") {
		t.Fatalf("link %s", link)
	}
	result := runWith(t, link, nil, nil, "read", "--passphrase-file", wrong)
	if result.code != exitError || !strings.Contains(result.stderr, "wrong passphrase; 4 attempts left") {
		t.Fatalf("wrong passphrase: %d %q", result.code, result.stderr)
	}
	if result = runWith(t, link, nil, nil, "read"); result.code != exitError ||
		!strings.Contains(result.stderr, "needs a passphrase") {
		t.Fatalf("no passphrase: %d %q", result.code, result.stderr)
	}
	paths := readPaths(t, runWith(t, link, map[string]string{envPassphrase: right}, nil, "read"))
	assertPrivate(t, paths[0], "behind a passphrase")
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
	if result.code != exitUsage || !strings.Contains(result.stderr, "unknown or incomplete flag") {
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
		{[]string{"short", "short"}, "at least 12 characters"},
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
		if result.code != exitError || !strings.Contains(result.stderr, testCase.want) ||
			instance.requests.Load() != 0 {
			t.Fatalf("%v: %d %q, %d requests", testCase.secrets, result.code, result.stderr, instance.requests.Load())
		}
	}
	terminal := &scriptedTerminal{secrets: []string{"twelve chars", "twelve chars"}}
	result := runWith(t, "text", nil, terminal, "create", "--server", instance.url, "--ask-passphrase")
	if result.code != exitOK ||
		!strings.Contains(result.stderr, "warning: a passphrase of at least 4 words and 16 characters") {
		t.Fatalf("a weak but valid passphrase: %d %q", result.code, result.stderr)
	}
	link, _, _ := strings.Cut(result.stdout, "\n")
	paths := readPaths(t, runWith(t, link, nil, &scriptedTerminal{secrets: []string{"twelve chars"}}, "read"))
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
	); result.code != exitOK ||
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
	if result.code != exitOK || len(lines) != 3 || strings.Contains(lines[1], "#") {
		t.Fatalf("create --print-manage-token: %d %q", result.code, result.stdout)
	}
	if burned := runWith(
		t,
		"",
		map[string]string{envLink: lines[0]},
		nil,
		"burn",
	); burned.code != exitOK ||
		burned.stdout != "deleted\n" {
		t.Fatalf("burn: %d %q %q", burned.code, burned.stdout, burned.stderr)
	}
	status := runWith(t, lines[1]+"\n"+lines[2]+"\n", nil, nil, "status", "--json")
	if status.code != exitOK || !strings.Contains(status.stdout, `"state":"deleted"`) {
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
		if result.code != exitOK {
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
		if result.code != exitUsage || strings.Contains(result.stderr, fragment) {
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
		"server":             true,
		"from-file":          true,
		"ttl":                true,
		"opens":              true,
		"passphrase-file":    true,
		"ask-passphrase":     true,
		"manage-token-file":  true,
		"print-manage-token": true,
		"link-file":          true,
		"out":                true,
		"stdout":             true,
		"json":               true,
		"allow-header":       true,
		"reason-file":        true,
	}
	for _, subcommand := range commands() {
		result := runWith(t, "", nil, nil, subcommand.name, "-h")
		if result.code != exitOK || result.stderr != "" {
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
	paths := readPaths(t, runWith(t, viaProxy, nil, nil, "read"))
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
		if result.code != exitError {
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
	if result.code != exitError || !strings.Contains(result.stderr, "redirect") || target.requests.Load() != 0 {
		t.Fatalf("a redirect was followed: %d %q, %d requests", result.code, result.stderr, target.requests.Load())
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
		"kdf profile": {[]string{"--passphrase-file", passphrase}, func(body string) string { return strings.Replace(body, `"kdf_id":1`, `"kdf_id":0`, 1) }, "does not match"},
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
			map[string]string{envPassphrase: passphrase},
			nil,
			"read",
		)
		if result.code != exitError || !strings.Contains(result.stderr, testCase.want) {
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
	if result.code != exitOK || !strings.Contains(result.stdout, "#1.1.") {
		t.Fatalf("a passphrase was dropped on the server's word: %d %q %q", result.code, result.stdout, result.stderr)
	}
}

func TestReadNeverLosesAMessageToANameClash(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	out := t.TempDir()
	first, _ := createText(t, instance, "first")
	second, _ := createText(t, instance, "second")
	firstPaths := readPaths(t, runWith(t, first, nil, nil, "read", "--out", out))
	secondPaths := readPaths(t, runWith(t, second, nil, nil, "read", "--out", out))
	assertPrivate(t, firstPaths[0], "first")
	assertPrivate(t, secondPaths[0], "second")
	apiClient, err := client.New(instance.url)
	if err != nil {
		t.Fatal(err)
	}
	text := "beside a clashing attachment"
	message := content{
		meta: core.Meta{Text: &text, File: &core.FileMeta{Name: "MESSAGE.TXT", Mime: "text/plain", Size: 4}},
		file: []byte("file"),
	}
	created, err := createOnce(
		t.Context(),
		&Environment{Stderr: io.Discard},
		apiClient,
		&createOptions{opens: 1},
		"",
		&message,
	)
	if err != nil {
		t.Fatal(err)
	}
	paths := readPaths(t, runWith(t, created.link, nil, nil, "read", "--out", out))
	if len(paths) != 2 || filepath.Base(paths[0]) != textFileName ||
		filepath.Base(paths[1]) != "attachment-MESSAGE.TXT" {
		t.Fatalf("text and attachment: %v", paths)
	}
	assertPrivate(t, paths[0], text)
	assertPrivate(t, paths[1], "file")
}

func TestReadChecksTheOutputBeforeItSpendsAnOpen(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "still there")
	before := instance.requests.Load()
	result := runWith(t, link, nil, nil, "read", "--out", filepath.Join(t.TempDir(), "missing"))
	if result.code != exitError || instance.requests.Load() != before {
		t.Fatalf("a missing --out: exit %d, %d requests", result.code, instance.requests.Load()-before)
	}
	out := t.TempDir()
	if result = runWith(t, link, nil, nil, "read", "--stdout", "--out", out); result.code != exitOK ||
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
	for name, field := range map[string][2]string{"kdf": {`"kdf_id":0`, `"kdf_id":1`}, "flags": {`"flags":0`, `"flags":1`}} {
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
					Set("X-Nylzo-Policy", core.Encode([]byte(strings.Replace(string(raw), field[0], field[1], 1))))
			}
			w.WriteHeader(response.StatusCode)
			io.Copy(w, response.Body)
		}))
		result := runWith(t, strings.Replace(link, instance.url, proxy.URL, 1), nil, nil, "read")
		proxy.Close()
		if result.code != exitError || !strings.Contains(result.stderr, "different policy") {
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
			if creates.Add(1) == 1 {
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"error":"conflict"}`))
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
	if result.code != exitOK || creates.Load() != 2 {
		t.Fatalf("create after a conflict: %d %q, %d creates", result.code, result.stderr, creates.Load())
	}
	distinct := 0
	ids.Range(func(any, any) bool { distinct++; return true })
	if distinct != 2 {
		t.Fatal("the retry reused the refused create header")
	}
	link, _, _ := strings.Cut(result.stdout, "\n")
	paths := readPaths(t, runWith(t, link, nil, nil, "read"))
	assertPrivate(t, paths[0], "second try")
	if metadata.Load() != 2 {
		t.Fatalf("metadata was asked %d times, want one retry after the 404", metadata.Load())
	}
}

func TestReadAcksAndSendsNoVerifierWithoutAPassphrase(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	manage := filepath.Join(t.TempDir(), "manage")
	link, _ := createText(t, instance, "acked", "--manage-token-file", manage)
	var verifier atomic.Bool
	instance.hold = func(r *http.Request) bool {
		if r.Header.Get("X-Nylzo-Verifier") != "" {
			verifier.Store(true)
		}
		return false
	}
	readPaths(t, runWith(t, "\n  "+link+"  \n", nil, nil, "read"))
	if verifier.Load() {
		t.Fatal("a message without a passphrase got a verifier header")
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
		"passphrase in the file variable": {map[string]string{envPassphrase: secret}, []string{"read"}},
		"token in the file variable":      {map[string]string{envManageToken: token}, []string{"status"}},
		"passphrase as a file":            {nil, []string{"read", "--passphrase-file", secret}},
		"passphrase as a duration":        {nil, []string{"create", "--server", instance.url, "--ttl", secret}},
		"token as a count":                {nil, []string{"create", "--server", instance.url, "--opens", token}},
		"link with trailing punctuation":  {nil, []string{"read", "--link-file", link + ")"}},
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
		if result.code == exitOK || strings.Contains(result.stderr, secret) || strings.Contains(result.stderr, token) ||
			strings.Contains(result.stderr, fragment) {
			t.Fatalf("%s: exit %d, stderr %q", name, result.code, result.stderr)
		}
		if strings.HasPrefix(name, "link with") && result.code != exitUsage {
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
		map[string]string{envLink: link, envPassphrase: writeSecretFile(t, "correct horse battery staple")},
		nil,
		"read",
	)
	paths := readPaths(t, result)
	assertPrivate(t, paths[0], "first line")
	if !strings.Contains(result.stderr, "named by "+envLink) ||
		!strings.Contains(result.stderr, "named by "+envPassphrase) {
		t.Fatalf("read did not say it used the variables: %q", result.stderr)
	}
}

func TestStdoutKeepsControlCharactersOffATerminal(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "safe\x1b]0;pwned\x07text")
	result := runWith(t, link, nil, &scriptedTerminal{stdout: true}, "read", "--stdout")
	if result.code != exitOK || strings.Contains(result.stdout, "\x1b") ||
		!strings.Contains(result.stderr, "control characters") {
		t.Fatalf("read --stdout to a terminal: %d %q %q", result.code, result.stdout, result.stderr)
	}
	assertPrivate(t, strings.TrimSpace(result.stdout), "safe\x1b]0;pwned\x07text")
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
	if result.code != exitError || strings.ContainsAny(result.stderr, "\x1b\x07") ||
		strings.Contains(result.stderr, "evil.example") {
		t.Fatalf("server text on stderr: %d %q", result.code, result.stderr)
	}
	cert := &x509.Certificate{DNSNames: []string{"\x1b]0;pwned\x07", "\x1b[2K\rnylzo: the message was saved"}}
	if explained := explain(
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
	if got := printable("a\x1b[2K\u202eb\x07"); got != `a\u001b[2K\u202eb\u0007` {
		t.Fatalf("printable: %q", got)
	}
}

func TestNamesThatFoldTogetherDoNotClash(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	apiClient, err := client.New(instance.url)
	if err != nil {
		t.Fatal(err)
	}
	text := "beside a folded clash"
	for _, name := range []string{"Meßage.txt", "meſſage.txt", "MESSAGE.TXT"} {
		message := content{
			meta: core.Meta{Text: &text, File: &core.FileMeta{Name: name, Mime: "text/plain", Size: 4}},
			file: []byte("file"),
		}
		created, createErr := createOnce(
			t.Context(),
			&Environment{Stderr: io.Discard},
			apiClient,
			&createOptions{opens: 1},
			"",
			&message,
		)
		if createErr != nil {
			t.Fatal(createErr)
		}
		paths := readPaths(t, runWith(t, created.link, nil, nil, "read"))
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
		&Environment{Stdout: io.Discard, Stderr: &stderr, Terminal: noTerminal{}},
		locked,
		base,
		false,
		core.Payload{Meta: core.Meta{Text: &text}},
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
		&Environment{Stdout: io.Discard, Stderr: io.Discard, Terminal: noTerminal{}},
		full,
		full,
		false,
		core.Payload{Meta: core.Meta{Text: &text}},
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
	result := runWith(t, "", map[string]string{envManageToken: manage}, nil, "status")
	if result.code != exitOK || !strings.Contains(result.stderr, "named by "+envManageToken) {
		t.Fatalf("status from the variable: %d %q", result.code, result.stderr)
	}
}

func TestASecondSignalStopsTheCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals")
	}
	t.Parallel()
	stdin, keep, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer keep.Close()
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
	process.Process.Signal(syscall.SIGTERM)
	time.Sleep(300 * time.Millisecond)
	process.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		process.Process.Kill()
		t.Fatal("two signals did not stop a command blocked on stdin")
	}
}

func TestFailureMessagesAreEscaped(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	failf(&Environment{Stderr: &stderr}, "%s", "spoof\x1b]0;x\x07\rnylzo: saved")
	if strings.ContainsAny(stderr.String(), "\x1b\x07\r") {
		t.Fatalf("raw control characters on stderr: %q", stderr.String())
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
	result := runWith(t, link, nil, nil, "read", "--out", locked)
	if result.code != exitError || strings.Contains(result.stderr, "correct horse") {
		t.Fatalf("read into an unwritable --out: %d %q", result.code, result.stderr)
	}
	paths := readPaths(t, runWith(t, link, nil, nil, "read"))
	assertPrivate(t, paths[0], "kept")
}
