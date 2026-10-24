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

	"github.com/misaon/nylzo/internal/client"
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
	dir      string
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
	ts := &testServer{dir: dir, release: make(chan struct{})}
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
	stdout  bool
}

func (s *scriptedTerminal) StdinIsTerminal() bool { return false }

func (s *scriptedTerminal) StdoutIsTerminal() bool { return s.stdout }

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
	code := Run(args, Env{Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr, Getenv: env(vars), Terminal: term, Version: "test", TempDir: t.TempDir()})
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
	ended := runWith(t, "", nil, nil, "status", "--manage-token-file", manage)
	if ended.code != exitOK || !strings.Contains(ended.stdout, "state: cancelled\n") || !strings.Contains(ended.stdout, "\nended: ") ||
		!strings.HasSuffix(ended.stdout, "\nopens and failed passphrase attempts: no longer kept\n") || strings.Contains(ended.stdout, "of 0 used") {
		t.Fatalf("status after cancel: %d %q", ended.code, ended.stdout)
	}
}

func TestCreateSealsEveryMessageWithItsOwnIV(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	createText(t, ts, "the same text")
	createText(t, ts, "the same text")
	entries, err := os.ReadDir(filepath.Join(ts.dir, disk.Dir))
	if err != nil {
		t.Fatal(err)
	}
	var ivs [][]byte
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		object, err := os.ReadFile(filepath.Join(ts.dir, disk.Dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		h, err := core.ParseHeader(object)
		if err != nil {
			t.Fatalf("a stored blob is not an envelope: %v", err)
		}
		ivs = append(ivs, h.IV[:])
	}
	if len(ivs) != 2 || bytes.Equal(ivs[0], ivs[1]) {
		t.Fatalf("stored IVs %x, want two different ones", ivs)
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
	if len(paths) != 1 || filepath.Dir(filepath.Dir(paths[0])) != out || filepath.Base(paths[0]) != "reportgpj.exe" {
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
		"manage-token-file": true, "print-manage-token": true, "link-file": true, "out": true, "stdout": true, "json": true, "allow-header": true, "reason-file": true,
	}
	for _, c := range commands() {
		res := runWith(t, "", nil, nil, c.name, "-h")
		if res.code != exitOK || res.stderr != "" {
			t.Fatalf("%s -h: exit %d, stderr %q", c.name, res.code, res.stderr)
		}
		for _, m := range flagLine.FindAllStringSubmatch(res.stdout, -1) {
			if !allowed[m[1]] {
				t.Fatalf("%s has the flag -%s, which is not on the list of flags known to carry no secret", c.name, m[1])
			}
		}
	}
}

func TestProcessListShowsNoSecretSmoke(t *testing.T) {
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
		req.ContentLength = r.ContentLength
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
		"state":       {nil, func(b string) string { return strings.Replace(b, `"state":"available"`, `"state":"opened"`, 1) }, "already opened"},
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

func TestAServerCannotSwitchOffThePassphrase(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	proxy := rewritingProxy(t, ts.url, func(r *http.Request, body []byte) []byte {
		if r.URL.Path == "/api/v1/instance" {
			return []byte(strings.Replace(string(body), `"default_kdf_id":1`, `"default_kdf_id":0`, 1))
		}
		return body
	})
	res := runWith(t, "secret", nil, nil, "create", "--server", proxy, "--passphrase-file", writeSecretFile(t, "correct horse battery staple"))
	if res.code != exitOK || !strings.Contains(res.stdout, "#1.1.") {
		t.Fatalf("a passphrase was dropped on the server's word: %d %q %q", res.code, res.stdout, res.stderr)
	}
}

func TestReadNeverLosesAMessageToANameClash(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	out := t.TempDir()
	first, _ := createText(t, ts, "first")
	second, _ := createText(t, ts, "second")
	a := readPaths(t, runWith(t, first, nil, nil, "read", "--out", out))
	b := readPaths(t, runWith(t, second, nil, nil, "read", "--out", out))
	assertPrivate(t, a[0], "first")
	assertPrivate(t, b[0], "second")
	c, err := client.New(ts.url)
	if err != nil {
		t.Fatal(err)
	}
	text := "beside a clashing attachment"
	msg := content{meta: core.Meta{Text: &text, File: &core.FileMeta{Name: "MESSAGE.TXT", Mime: "text/plain", Size: 4}}, file: []byte("file")}
	created, err := createOnce(t.Context(), Env{Stderr: io.Discard}, c, createOptions{opens: 1}, core.KDFNone, "", msg)
	if err != nil {
		t.Fatal(err)
	}
	paths := readPaths(t, runWith(t, created.link, nil, nil, "read", "--out", out))
	if len(paths) != 2 || filepath.Base(paths[0]) != textFileName || filepath.Base(paths[1]) != "attachment-MESSAGE.TXT" {
		t.Fatalf("text and attachment: %v", paths)
	}
	assertPrivate(t, paths[0], text)
	assertPrivate(t, paths[1], "file")
}

func TestReadChecksTheOutputBeforeItSpendsAnOpen(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	link, _ := createText(t, ts, "still there")
	before := ts.requests.Load()
	res := runWith(t, link, nil, nil, "read", "--out", filepath.Join(t.TempDir(), "missing"))
	if res.code != exitError || ts.requests.Load() != before {
		t.Fatalf("a missing --out: exit %d, %d requests", res.code, ts.requests.Load()-before)
	}
	out := t.TempDir()
	if res := runWith(t, link, nil, nil, "read", "--stdout", "--out", out); res.code != exitOK || res.stdout != "still there\n" {
		t.Fatalf("read --stdout: %d %q %q", res.code, res.stdout, res.stderr)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("read --stdout left %d empty directories behind", len(entries))
	}
}

func TestLiedPolicyHeaderIsRefused(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	for name, field := range map[string][2]string{"kdf": {`"kdf_id":0`, `"kdf_id":2`}, "flags": {`"flags":0`, `"flags":1`}} {
		link, _ := createText(t, ts, "policy header")
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req, _ := http.NewRequestWithContext(r.Context(), r.Method, ts.url+r.URL.RequestURI(), r.Body)
			req.Header = r.Header.Clone()
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			maps.Copy(w.Header(), resp.Header)
			if policy := resp.Header.Get("X-Nylzo-Policy"); policy != "" {
				raw, _ := base64.RawURLEncoding.DecodeString(policy)
				w.Header().Set("X-Nylzo-Policy", core.Encode([]byte(strings.Replace(string(raw), field[0], field[1], 1))))
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
		}))
		res := runWith(t, strings.Replace(link, ts.url, proxy.URL, 1), nil, nil, "read")
		proxy.Close()
		if res.code != exitError || !strings.Contains(res.stderr, "different policy") {
			t.Fatalf("%s: %d %q", name, res.code, res.stderr)
		}
	}
}

func TestCreateRetriesWithANewIDAndSignedRequestsRetryA404(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
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
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, ts.url+r.URL.RequestURI(), r.Body)
		req.Header = r.Header.Clone()
		req.ContentLength = r.ContentLength
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		maps.Copy(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	t.Cleanup(proxy.Close)
	res := runWith(t, "second try", nil, nil, "create", "--server", proxy.URL)
	if res.code != exitOK || creates.Load() != 2 {
		t.Fatalf("create after a conflict: %d %q, %d creates", res.code, res.stderr, creates.Load())
	}
	distinct := 0
	ids.Range(func(any, any) bool { distinct++; return true })
	if distinct != 2 {
		t.Fatal("the retry reused the refused create header")
	}
	link, _, _ := strings.Cut(res.stdout, "\n")
	paths := readPaths(t, runWith(t, link, nil, nil, "read"))
	assertPrivate(t, paths[0], "second try")
	if metadata.Load() != 2 {
		t.Fatalf("metadata was asked %d times, want one retry after the 404", metadata.Load())
	}
}

func TestReadAcksAndSendsNoVerifierWithoutAPassphrase(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	manage := filepath.Join(t.TempDir(), "manage")
	link, _ := createText(t, ts, "acked", "--manage-token-file", manage)
	var verifier atomic.Bool
	ts.hold = func(r *http.Request) bool {
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
	ts := newTestServer(t, nil)
	link, _ := createText(t, ts, "quiet")
	_, fragment, _ := strings.Cut(link, "#")
	secret := "correct horse battery staple"
	token := core.Encode(make([]byte, 32))
	for name, c := range map[string]struct {
		vars map[string]string
		args []string
	}{
		"passphrase in the file variable": {map[string]string{envPassphrase: secret}, []string{"read"}},
		"token in the file variable":      {map[string]string{envManageToken: token}, []string{"status"}},
		"passphrase as a file":            {nil, []string{"read", "--passphrase-file", secret}},
		"passphrase as a duration":        {nil, []string{"create", "--server", ts.url, "--ttl", secret}},
		"token as a count":                {nil, []string{"create", "--server", ts.url, "--opens", token}},
		"link with trailing punctuation":  {nil, []string{"read", "--link-file", link + ")"}},
		"link with a carriage return":     {nil, []string{"burn", "--link-file=" + link + "\r"}},
		"passphrase as --out":             {nil, []string{"read", "--out", filepath.Join(t.TempDir(), secret)}},
		"token as the manage file":        {nil, []string{"create", "--server", ts.url, "--manage-token-file", existing(t, token)}},
		"token as a command":              {nil, []string{token}},
		"passphrase as a command":         {nil, []string{secret}},
	} {
		stdin := link
		if c.args[0] == "status" {
			stdin = ""
		}
		if name == "passphrase in the file variable" || name == "passphrase as a file" {
			guarded, _ := createText(t, ts, "guarded", "--passphrase-file", writeSecretFile(t, secret))
			stdin = guarded
		}
		res := runWith(t, stdin, c.vars, nil, c.args...)
		if res.code == exitOK || strings.Contains(res.stderr, secret) || strings.Contains(res.stderr, token) || strings.Contains(res.stderr, fragment) {
			t.Fatalf("%s: exit %d, stderr %q", name, res.code, res.stderr)
		}
		if strings.HasPrefix(name, "link with") && res.code != exitUsage {
			t.Fatalf("%s: the link in an argument was not refused up front: exit %d", name, res.code)
		}
	}
}

func TestPassphraseFileUsesItsFirstLineAndVariablesAreNamed(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	file := writeSecretFile(t, "correct horse battery staple\r\nnote for Bob\n")
	link, _ := createText(t, ts, "first line", "--passphrase-file", file)
	res := runWith(t, "", map[string]string{envLink: link, envPassphrase: writeSecretFile(t, "correct horse battery staple")}, nil, "read")
	paths := readPaths(t, res)
	assertPrivate(t, paths[0], "first line")
	if !strings.Contains(res.stderr, "named by "+envLink) || !strings.Contains(res.stderr, "named by "+envPassphrase) {
		t.Fatalf("read did not say it used the variables: %q", res.stderr)
	}
}

func TestStdoutKeepsControlCharactersOffATerminal(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	link, _ := createText(t, ts, "safe\x1b]0;pwned\x07text")
	res := runWith(t, link, nil, &scriptedTerminal{stdout: true}, "read", "--stdout")
	if res.code != exitOK || strings.Contains(res.stdout, "\x1b") || !strings.Contains(res.stderr, "control characters") {
		t.Fatalf("read --stdout to a terminal: %d %q %q", res.code, res.stdout, res.stderr)
	}
	assertPrivate(t, strings.TrimSpace(res.stdout), "safe\x1b]0;pwned\x07text")
}

func TestWritePrivateStaysInItsDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"../escape", "sub/../../escape"} {
		if _, err := writePrivate(dir, name, []byte("x")); err == nil {
			t.Fatalf("wrote %q outside %s", name, dir)
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
	res := runWith(t, "text", nil, nil, "create", "--server", hostile.URL)
	if res.code != exitError || strings.ContainsAny(res.stderr, "\x1b\x07") || strings.Contains(res.stderr, "evil.example") {
		t.Fatalf("server text on stderr: %d %q", res.code, res.stderr)
	}
	cert := &x509.Certificate{DNSNames: []string{"\x1b]0;pwned\x07", "\x1b[2K\rnylzo: the message was saved"}}
	if msg := explain(&url.Error{Op: "Get", URL: "https://localhost/", Err: &tls.CertificateVerificationError{Err: x509.HostnameError{Certificate: cert, Host: "localhost"}}}); strings.ContainsAny(msg, "\x1b\x07\r") {
		t.Fatalf("certificate names reach the terminal: %q", msg)
	}
	if got := printable("a\x1b[2K\u202eb\x07"); got != `a\u001b[2K\u202eb\u0007` {
		t.Fatalf("printable: %q", got)
	}
}

func TestNamesThatFoldTogetherDoNotClash(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	c, err := client.New(ts.url)
	if err != nil {
		t.Fatal(err)
	}
	text := "beside a folded clash"
	for _, name := range []string{"Meßage.txt", "meſſage.txt", "MESSAGE.TXT"} {
		msg := content{meta: core.Meta{Text: &text, File: &core.FileMeta{Name: name, Mime: "text/plain", Size: 4}}, file: []byte("file")}
		created, err := createOnce(t.Context(), Env{Stderr: io.Discard}, c, createOptions{opens: 1}, core.KDFNone, "", msg)
		if err != nil {
			t.Fatal(err)
		}
		paths := readPaths(t, runWith(t, created.link, nil, nil, "read"))
		if len(paths) != 2 || !strings.HasPrefix(filepath.Base(paths[1]), attachmentPrefix) {
			t.Fatalf("%s: %v", name, paths)
		}
		assertPrivate(t, paths[1], "file")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "taken.bin"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := writeAll(dir, []outputFile{{"taken.bin", []byte("new")}})
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
	paths, err := saveMessage(Env{Stdout: io.Discard, Stderr: &stderr, Terminal: noTerminal{}}, locked, base, false, core.Payload{Meta: core.Meta{Text: &text}})
	if err != nil || len(paths) != 1 || filepath.Dir(filepath.Dir(paths[0])) != base || filepath.Dir(paths[0]) == locked {
		t.Fatalf("fallback wrote %v (%v)", paths, err)
	}
	assertPrivate(t, paths[0], text)
	full := filepath.Join(t.TempDir(), "full")
	if err := os.Mkdir(full, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := saveMessage(Env{Stdout: io.Discard, Stderr: io.Discard, Terminal: noTerminal{}}, full, full, false, core.Payload{Meta: core.Meta{Text: &text}}); err == nil {
		t.Fatal("a write with nowhere to go succeeded")
	}
	partial := t.TempDir()
	if err := os.Mkdir(filepath.Join(partial, "a.bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(partial, attachmentPrefix+"a.bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := writeAll(partial, []outputFile{{textFileName, []byte("x")}, {"a.bin", []byte("y")}}); err == nil {
		t.Fatal("writeAll succeeded with nowhere to put the attachment")
	}
	if _, err := os.Stat(filepath.Join(partial, textFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a failed set of files left the text behind")
	}
}

func TestControlCharactersAndTheTerminalGate(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	bidi, _ := createText(t, ts, "looks\u202esafe")
	if res := runWith(t, bidi, nil, &scriptedTerminal{stdout: true}, "read", "--stdout"); strings.Contains(res.stdout, "\u202e") || !strings.Contains(res.stderr, "control characters") {
		t.Fatalf("a format character went to the terminal: %q %q", res.stdout, res.stderr)
	}
	piped, _ := createText(t, ts, "to\x1ba pipe")
	if res := runWith(t, piped, nil, nil, "read", "--stdout"); res.stdout != "to\x1ba pipe\n" {
		t.Fatalf("text for a pipe was changed: %q %q", res.stdout, res.stderr)
	}
}

func TestManageTokenVariableIsNamed(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	manage := filepath.Join(t.TempDir(), "manage")
	createText(t, ts, "named", "--manage-token-file", manage)
	res := runWith(t, "", map[string]string{envManageToken: manage}, nil, "status")
	if res.code != exitOK || !strings.Contains(res.stderr, "named by "+envManageToken) {
		t.Fatalf("status from the variable: %d %q", res.code, res.stderr)
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
	cmd := exec.Command(os.Args[0], "burn")
	cmd.Env = append(os.Environ(), reexecEnv+"=1")
	cmd.Stdin = stdin
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)
	cmd.Process.Signal(syscall.SIGTERM)
	time.Sleep(300 * time.Millisecond)
	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("two signals did not stop a command blocked on stdin")
	}
}

func TestFailureMessagesAreEscaped(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	failf(Env{Stderr: &stderr}, "%s", "spoof\x1b]0;x\x07\rnylzo: saved")
	if strings.ContainsAny(stderr.String(), "\x1b\x07\r") {
		t.Fatalf("raw control characters on stderr: %q", stderr.String())
	}
}

func TestAnUnwritableOutDirectoryIsNotNamed(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	t.Parallel()
	ts := newTestServer(t, nil)
	link, _ := createText(t, ts, "kept")
	locked := filepath.Join(t.TempDir(), "correct horse battery staple")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	res := runWith(t, link, nil, nil, "read", "--out", locked)
	if res.code != exitError || strings.Contains(res.stderr, "correct horse") {
		t.Fatalf("read into an unwritable --out: %d %q", res.code, res.stderr)
	}
	paths := readPaths(t, runWith(t, link, nil, nil, "read"))
	assertPrivate(t, paths[0], "kept")
}
