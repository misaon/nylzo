package cli

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const reexecEnv = "NYLZO_CLI_TEST_REEXEC"

func TestMain(m *testing.M) {
	if os.Getenv(reexecEnv) == "1" {
		os.Exit(Run(os.Args[1:], Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv, Terminal: noTerminal{}}))
	}
	os.Exit(m.Run())
}

type testServer struct {
	url      string
	requests atomic.Int64
	hold     func(*http.Request) bool
	release  chan struct{}
}

func newTestServer(t *testing.T, change func(*server.Config)) *testServer {
	t.Helper()
	dir := t.TempDir()
	cfg := server.Defaults()
	cfg.DataDir = dir
	cfg.Pepper = bytes.Repeat([]byte{7}, 32)
	cfg.Rates.BucketPerSecond, cfg.Rates.BucketBurst = 1<<20, 1<<20
	cfg.Rates.SourcePerSecond, cfg.Rates.SourceBurst = 1<<20, 1<<20
	cfg.Rates.CreatePerHour, cfg.Rates.CreatePerDay, cfg.Rates.ReadPerHour = 1000, 1000, 1000
	if change != nil {
		change(&cfg)
	}
	st, err := sqlite.Open(t.Context(), filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(cfg, st, blobs, server.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	ts := &testServer{release: make(chan struct{})}
	handler := srv.Handler()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.requests.Add(1)
		if ts.hold != nil && ts.hold(r) {
			<-ts.release
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(hs.Close)
	t.Cleanup(srv.Close)
	ts.url = hs.URL
	return ts
}

type scriptedTerminal struct {
	mu      sync.Mutex
	secrets []string
}

func (s *scriptedTerminal) StdinIsTerminal() bool { return false }

func (s *scriptedTerminal) ReadSecret(string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.secrets) == 0 {
		return nil, ErrNoTerminal
	}
	next := s.secrets[0]
	s.secrets = s.secrets[1:]
	return []byte(next), nil
}

type result struct {
	code   int
	stdout string
	stderr string
}

func runWith(t *testing.T, stdin string, vars map[string]string, term Terminal, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if term == nil {
		term = noTerminal{}
	}
	code := Run(args, Env{Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr, Getenv: env(vars), Terminal: term, Version: "test"})
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func createText(t *testing.T, ts *testServer, text string, args ...string) (string, result) {
	t.Helper()
	res := runWith(t, text, nil, nil, append([]string{"create", "--server", ts.url}, args...)...)
	if res.code != exitOK {
		t.Fatalf("create: exit %d, %s", res.code, res.stderr)
	}
	link, _, _ := strings.Cut(res.stdout, "\n")
	return link, res
}

func readPaths(t *testing.T, res result) []string {
	t.Helper()
	if res.code != exitOK {
		t.Fatalf("read: exit %d, %s", res.code, res.stderr)
	}
	return strings.Fields(res.stdout)
}

func assertPrivate(t *testing.T, path string, want string) {
	t.Helper()
	assertPrivateIn(t, path, want, []string{path, filepath.Dir(path)})
}

func assertPrivateFile(t *testing.T, path string, want string) {
	t.Helper()
	assertPrivateIn(t, path, want, []string{path})
}

func assertPrivateIn(t *testing.T, path string, want string, private []string) {
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
	for _, p := range private {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is open to other users: %v", p, info.Mode().Perm())
		}
	}
}

func TestCreateAndReadText(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	manage := filepath.Join(t.TempDir(), "manage")
	link, created := createText(t, ts, "tajná zpráva\nna dva řádky", "--manage-token-file", manage, "--ttl", "1h", "--opens", "2")
	if !strings.HasPrefix(link, ts.url+"/m/") || strings.Count(created.stdout, "\n") != 1 {
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
	if status.code != exitOK || !strings.Contains(status.stdout, "state: available") || !strings.Contains(status.stdout, "opens: 1 of 2 used") {
		t.Fatalf("status: %d %s %s", status.code, status.stdout, status.stderr)
	}
	if res := runWith(t, "", nil, nil, "cancel", "--manage-token-file", manage); res.code != exitOK || res.stdout != "cancelled\n" {
		t.Fatalf("cancel: %d %q %q", res.code, res.stdout, res.stderr)
	}
	if res := runWith(t, link, nil, nil, "read"); res.code != exitError || !strings.Contains(res.stderr, "the sender cancelled the message") {
		t.Fatalf("read after cancel: %d %q", res.code, res.stderr)
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
	ts := newTestServer(t, nil)
	right := writeSecretFile(t, "correct horse battery staple\n")
	wrong := writeSecretFile(t, "wrong horse battery staple")
	for _, kdf := range []string{"scrypt", "pbkdf2"} {
		link, _ := createText(t, ts, "behind a passphrase", "--passphrase-file", right, "--kdf", kdf)
		if !strings.Contains(link, "#1."+map[string]string{"scrypt": "1", "pbkdf2": "2"}[kdf]+".") {
			t.Fatalf("%s link %s", kdf, link)
		}
		res := runWith(t, link, nil, nil, "read", "--passphrase-file", wrong)
		if res.code != exitError || !strings.Contains(res.stderr, "wrong passphrase; 4 attempts left") {
			t.Fatalf("%s wrong passphrase: %d %q", kdf, res.code, res.stderr)
		}
		if res := runWith(t, link, nil, nil, "read"); res.code != exitError || !strings.Contains(res.stderr, "needs a passphrase") {
			t.Fatalf("%s no passphrase: %d %q", kdf, res.code, res.stderr)
		}
		paths := readPaths(t, runWith(t, link, map[string]string{envPassphrase: right}, nil, "read"))
		assertPrivate(t, paths[0], "behind a passphrase")
	}
}

func TestAskedPassphraseMustMatchAndBeStrong(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	for _, c := range []struct {
		secrets []string
		want    string
	}{
		{[]string{"correct horse battery staple", "correct horse battery stapel"}, "the two passphrases differ"},
		{[]string{"short", "short"}, "at least 12 characters"},
	} {
		res := runWith(t, "text", nil, &scriptedTerminal{secrets: c.secrets}, "create", "--server", ts.url, "--ask-passphrase")
		if res.code != exitError || !strings.Contains(res.stderr, c.want) || ts.requests.Load() != 0 {
			t.Fatalf("%v: %d %q, %d requests", c.secrets, res.code, res.stderr, ts.requests.Load())
		}
	}
	term := &scriptedTerminal{secrets: []string{"twelve chars", "twelve chars"}}
	res := runWith(t, "text", nil, term, "create", "--server", ts.url, "--ask-passphrase")
	if res.code != exitOK || !strings.Contains(res.stderr, "warning: a passphrase of at least 4 words and 16 characters") {
		t.Fatalf("a weak but valid passphrase: %d %q", res.code, res.stderr)
	}
	link, _, _ := strings.Cut(res.stdout, "\n")
	paths := readPaths(t, runWith(t, link, nil, &scriptedTerminal{secrets: []string{"twelve chars"}}, "read"))
	assertPrivate(t, paths[0], "text")
}

func TestAttachmentAndStdout(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	src := filepath.Join(t.TempDir(), "report\u202egpj.exe")
	payload := bytes.Repeat([]byte{0, 1, 2, 250}, 300_000)
	if err := os.WriteFile(src, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	link, _ := createText(t, ts, "", "--from-file", src)
	out := t.TempDir()
	paths := readPaths(t, runWith(t, link, nil, nil, "read", "--out", out))
	if len(paths) != 1 || filepath.Dir(paths[0]) != out || filepath.Base(paths[0]) != "reportgpj.exe" {
		t.Fatalf("attachment written to %v", paths)
	}
	if got, _ := os.ReadFile(paths[0]); !bytes.Equal(got, payload) {
		t.Fatal("the attachment differs from the file that was sent")
	}
	text, _ := createText(t, ts, "to stdout")
	if res := runWith(t, text, nil, nil, "read", "--stdout"); res.code != exitOK || res.stdout != "to stdout\n" {
		t.Fatalf("read --stdout: %d %q %q", res.code, res.stdout, res.stderr)
	}
}

func TestBurnAndPrintedManageToken(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	res := runWith(t, "burn me", nil, nil, "create", "--server", ts.url, "--opens", "3", "--print-manage-token")
	lines := strings.Split(strings.TrimSpace(res.stdout), "\n")
	if res.code != exitOK || len(lines) != 3 || strings.Contains(lines[1], "#") {
		t.Fatalf("create --print-manage-token: %d %q", res.code, res.stdout)
	}
	if burned := runWith(t, "", map[string]string{envLink: lines[0]}, nil, "burn"); burned.code != exitOK || burned.stdout != "deleted\n" {
		t.Fatalf("burn: %d %q %q", burned.code, burned.stdout, burned.stderr)
	}
	status := runWith(t, lines[1]+"\n"+lines[2]+"\n", nil, nil, "status", "--json")
	if status.code != exitOK || !strings.Contains(status.stdout, `"state":"deleted"`) {
		t.Fatalf("status after burn: %d %q %q", status.code, status.stdout, status.stderr)
	}
}

func TestCreateSolvesProofOfWork(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(c *server.Config) {
		c.Rates.GlobalDailyCreates = 4
		c.PoWDifficulty = 2
	})
	for i := range 3 {
		res := runWith(t, "busy server", nil, nil, "create", "--server", ts.url)
		if res.code != exitOK {
			t.Fatalf("create %d: %d %q", i, res.code, res.stderr)
		}
		if i >= 2 && !strings.Contains(res.stderr, "solving a proof of work") {
			t.Fatalf("create %d did not solve a proof of work: %q", i, res.stderr)
		}
	}
}

func TestSecretsNeverTravelInArguments(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	link, _ := createText(t, ts, "never in argv")
	before := ts.requests.Load()
	head, fragment, _ := strings.Cut(link, "#")
	for _, args := range [][]string{
		{"read", link}, {"read", "--out", link}, {"burn", "--link-file=" + link}, {"create", "--server", link},
		{"status", "#" + fragment}, {"cancel", head + "#" + fragment}, {"create", "--server", ts.url, "my secret text"},
		{"read", head}, {"status", core.Encode(make([]byte, 32))}, {"create", "--server", ts.url, "--ttl", "1h", "correct horse battery staple"},
	} {
		res := runWith(t, "", nil, nil, args...)
		if res.code != exitUsage || strings.Contains(res.stderr, fragment) {
			t.Fatalf("%v: exit %d, stderr %q", args, res.code, res.stderr)
		}
	}
	if ts.requests.Load() != before {
		t.Fatal("a refused command still talked to the server")
	}
}

var flagLine = regexp.MustCompile(`(?m)^  -([a-z-]+)`)

func TestNoFlagTakesASecret(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"server": true, "from-file": true, "ttl": true, "opens": true, "passphrase-file": true, "ask-passphrase": true, "kdf": true,
		"manage-token-file": true, "print-manage-token": true, "link-file": true, "out": true, "stdout": true, "json": true,
	}
	for _, c := range commands() {
		res := runWith(t, "", nil, nil, c.name, "-h")
		for _, m := range flagLine.FindAllStringSubmatch(res.stderr, -1) {
			if !allowed[m[1]] {
				t.Fatalf("%s has the flag -%s, which is not on the list of flags known to carry no secret", c.name, m[1])
			}
		}
	}
}

func TestProcessListShowsNoSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ps on Windows")
	}
	ps, err := exec.LookPath("ps")
	if err != nil {
		t.Skip("no ps")
	}
	t.Parallel()
	ts := newTestServer(t, nil)
	passphrase := writeSecretFile(t, "correct horse battery staple")
	link, _ := createText(t, ts, "watched by ps", "--passphrase-file", passphrase)
	held := make(chan struct{})
	var once sync.Once
	ts.hold = func(r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/claim") {
			once.Do(func() { close(held) })
			return true
		}
		return false
	}
	cmd := exec.Command(os.Args[0], "read", "--passphrase-file", passphrase, "--out", t.TempDir())
	cmd.Env = append(os.Environ(), reexecEnv+"=1")
	cmd.Stdin = strings.NewReader(link)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(30 * time.Second):
		t.Fatal("the claim never arrived")
	}
	listing, err := exec.Command(ps, "-o", "args=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
	close(ts.release)
	waitErr := cmd.Wait()
	if err != nil {
		t.Fatal(err)
	}
	_, fragment, _ := strings.Cut(link, "#")
	if !strings.Contains(string(listing), "read") || strings.Contains(string(listing), fragment) || strings.Contains(string(listing), "correct horse") {
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

func (b *breakingWriter) Write(p []byte) (int, error) {
	if len(p) > b.limit {
		p = p[:b.limit]
	}
	n, _ := b.ResponseWriter.Write(p)
	b.ResponseWriter.(http.Flusher).Flush()
	b.limit -= n
	if b.limit <= 0 {
		panic(http.ErrAbortHandler)
	}
	return n, nil
}

func TestBrokenClaimResumesThroughTheLease(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	manage := filepath.Join(t.TempDir(), "manage")
	link, _ := createText(t, ts, strings.Repeat("resume ", 2000), "--manage-token-file", manage, "--opens", "2")
	var broke atomic.Bool
	srvURL := ts.url
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := srvURL + r.URL.RequestURI()
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		maps.Copy(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		w.(http.Flusher).Flush()
		if strings.HasSuffix(r.URL.Path, "/claim") && broke.CompareAndSwap(false, true) {
			io.Copy(&breakingWriter{ResponseWriter: w, limit: 100}, resp.Body)
			return
		}
		io.Copy(w, resp.Body)
	}))
	t.Cleanup(proxy.Close)
	viaProxy := strings.Replace(link, ts.url, proxy.URL, 1)
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
	for _, server := range []string{"http://nylzo.example", "https://nylzo.example/path", "https://user@nylzo.example", "https://nylzo.example/?q=1", "ftp://nylzo.example"} {
		res := runWith(t, "text", nil, nil, "create", "--server", server)
		if res.code != exitError {
			t.Fatalf("%s: exit %d %q", server, res.code, res.stderr)
		}
	}
}

func TestRedirectsAreRefused(t *testing.T) {
	t.Parallel()
	target := newTestServer(t, nil)
	redirect := httptest.NewServer(http.RedirectHandler(target.url+"/api/v1/instance", http.StatusFound))
	t.Cleanup(redirect.Close)
	res := runWith(t, "text", nil, nil, "create", "--server", redirect.URL)
	if res.code != exitError || !strings.Contains(res.stderr, "redirect") || target.requests.Load() != 0 {
		t.Fatalf("a redirect was followed: %d %q, %d requests", res.code, res.stderr, target.requests.Load())
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
		if _, err := parseManageToken(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestWritePrivateNeverOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := writePrivate(dir, "a.txt", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := writePrivate(dir, "a.txt", []byte("two")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrote an existing file: %v", err)
	}
	if got := mustRead(t, filepath.Join(dir, "a.txt")); got != "one" {
		t.Fatalf("file now holds %q", got)
	}
}

func rewritingProxy(t *testing.T, target string, rewrite func(*http.Request, []byte) []byte) string {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, target+r.URL.RequestURI(), r.Body)
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		body = rewrite(r, body)
		maps.Copy(w.Header(), resp.Header)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL
}

func TestLiedMetadataIsRefused(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	passphrase := writeSecretFile(t, "correct horse battery staple")
	for name, c := range map[string]struct {
		args    []string
		rewrite func(string) string
		want    string
	}{
		"opens_max":   {nil, func(b string) string { return strings.Replace(b, `"opens_max":2`, `"opens_max":3`, 1) }, "different policy"},
		"ttl_s":       {nil, func(b string) string { return strings.Replace(b, `"ttl_s":3600`, `"ttl_s":7200`, 1) }, "different policy"},
		"kdf profile": {[]string{"--passphrase-file", passphrase, "--kdf", "scrypt"}, func(b string) string { return strings.Replace(b, `"kdf_id":1`, `"kdf_id":2`, 1) }, "does not match"},
	} {
		manage := filepath.Join(t.TempDir(), "manage")
		link, _ := createText(t, ts, "policy", append([]string{"--opens", "2", "--ttl", "1h", "--manage-token-file", manage}, c.args...)...)
		proxy := rewritingProxy(t, ts.url, func(r *http.Request, body []byte) []byte {
			if r.Method == http.MethodGet && strings.Count(r.URL.Path, "/") == 4 {
				return []byte(c.rewrite(string(body)))
			}
			return body
		})
		res := runWith(t, strings.Replace(link, ts.url, proxy, 1), map[string]string{envPassphrase: passphrase}, nil, "read")
		if res.code != exitError || !strings.Contains(res.stderr, c.want) {
			t.Fatalf("%s: %d %q", name, res.code, res.stderr)
		}
		status := runWith(t, "", nil, nil, "status", "--manage-token-file", manage)
		if name == "kdf profile" && !strings.Contains(status.stdout, "opens: 0 of 2 used") {
			t.Fatalf("a profile mismatch spent an open: %s", status.stdout)
		}
	}
}
