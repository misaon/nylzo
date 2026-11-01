package local_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/local"
	"github.com/misaon/nylzo/internal/localclient"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	linkPath     = "/m/AAAAAAAAAAAAAAAAAAAAAA"
	linkFragment = "1.0.QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWY"
)

var handoffAddress = regexp.MustCompile(`url=(http://127\.0\.0\.1:\d+/open#[A-Z2-7]{26})"`)

type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

type run struct {
	stdout lockedBuffer
	stderr lockedBuffer
	opened chan string
	done   chan int
	cancel context.CancelFunc
}

func start(t *testing.T, options local.Options) *run {
	t.Helper()
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(instance.Close)
	build, err := webclient.Load(fstest.MapFS{
		"placeholder.html":  {Data: []byte("placeholder")},
		"dist/index.html":   {Data: []byte(`<!doctype html><head></head><body></body>`)},
		"dist/home.en.html": {Data: []byte(`<!doctype html><head></head><body><h1>en</h1></body>`)},
		"dist/home.cs.html": {Data: []byte(`<!doctype html><head></head><body><h1>cs</h1></body>`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	options.Instance, options.Build, options.Listen = instance.URL, build, localclient.DefaultAddress
	ctx, cancel := context.WithCancel(t.Context())
	current := &run{opened: make(chan string, 1), done: make(chan int, 1), cancel: cancel}
	environment := &command.Environment{
		Stdout:  &current.stdout,
		Stderr:  &current.stderr,
		Version: "test",
		Browser: func(target string) error {
			current.opened <- target
			return nil
		},
	}
	go func() { current.done <- local.Serve(ctx, environment, options) }()
	t.Cleanup(cancel)
	return current
}

func (r *run) target(t *testing.T) string {
	t.Helper()
	select {
	case target := <-r.opened:
		return target
	case code := <-r.done:
		t.Fatalf("the verified client stopped with %d: %s", code, r.stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("no browser was opened")
	}
	return ""
}

func (r *run) stop(t *testing.T) int {
	t.Helper()
	r.cancel()
	select {
	case code := <-r.done:
		return code
	case <-time.After(10 * time.Second):
		t.Fatal("the verified client did not stop")
		return 0
	}
}

func redeem(t *testing.T, address string) *http.Response {
	t.Helper()
	origin, token, _ := strings.Cut(address, localclient.HandoffPath+"#")
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, origin+localclient.HandoffPath, strings.NewReader(token))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestWebServesTheComposerForTheInstance(t *testing.T) {
	t.Parallel()
	current := start(t, local.Options{OpenBrowser: true})
	address := current.target(t)
	if !regexp.MustCompile(`^http://127\.0\.0\.1:\d+/$`).MatchString(address) || current.stdout.String() != address+"\n" {
		t.Fatalf("the composer must open on a random loopback port: %q, stdout %q", address, current.stdout.String())
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "<h1>en</h1>") {
		t.Fatalf("composer: %d %s", response.StatusCode, body)
	}
	if code := current.stop(t); code != command.ExitOK {
		t.Fatalf("stopping: %d %s", code, current.stderr.String())
	}
}

func TestOpenHandsTheLinkOverOnceAndKeepsItOutOfEveryArgument(t *testing.T) {
	t.Parallel()
	current := start(t, local.Options{
		OpenBrowser: true,
		Link:        &local.Link{Path: linkPath, Fragment: linkFragment, Lifetime: time.Minute},
	})
	page := current.target(t)
	if strings.Contains(page, "#") || strings.Contains(page, "open#") {
		t.Fatalf("the browser was opened with the handoff address in its arguments: %q", page)
	}
	content, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Stat(page)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.Stat(filepath.Dir(page))
	if err != nil {
		t.Fatal(err)
	}
	if file.Mode().Perm() != 0o600 || directory.Mode().Perm() != 0o700 {
		t.Fatalf("the handoff page must be private: %v %v", file.Mode(), directory.Mode())
	}
	match := handoffAddress.FindSubmatch(content)
	if match == nil || strings.Contains(string(content), linkFragment) || current.stdout.String() != "" {
		t.Fatalf("handoff page %s, stdout %q", content, current.stdout.String())
	}
	address := string(match[1])
	first := redeem(t, address)
	body, _ := io.ReadAll(first.Body)
	first.Body.Close()
	if first.StatusCode != http.StatusOK || !strings.Contains(string(body), linkFragment) {
		t.Fatalf("first redemption: %d %s", first.StatusCode, body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(current.stderr.String(), "the browser took the link") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, statErr := os.Stat(filepath.Dir(page)); !os.IsNotExist(statErr) {
		t.Fatalf("the handoff page must be gone once the browser took the link: %v", statErr)
	}
	second := redeem(t, address)
	second.Body.Close()
	if second.StatusCode != http.StatusNotFound {
		t.Fatalf("second redemption: %d", second.StatusCode)
	}
	for !strings.Contains(current.stderr.String(), "another program has the link") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(current.stderr.String(), "another program has the link") {
		t.Fatalf("a second request for the link must warn: %s", current.stderr.String())
	}
	if code := current.stop(t); code != command.ExitOK || strings.Contains(current.stderr.String(), linkFragment) {
		t.Fatalf("stopping: %d %s", code, current.stderr.String())
	}
}

func TestOpenWithoutABrowserPrintsTheHandoffAddressAndGivesUpInTime(t *testing.T) {
	t.Parallel()
	current := start(t, local.Options{
		Link: &local.Link{Path: linkPath, Fragment: linkFragment, Lifetime: 200 * time.Millisecond},
	})
	select {
	case code := <-current.done:
		if code != command.ExitError || !strings.Contains(current.stderr.String(), "no browser took the link") {
			t.Fatalf("expiry: %d %s", code, current.stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("an untaken handoff must end the command")
	}
	printed := strings.TrimSpace(current.stdout.String())
	if !regexp.MustCompile(`^http://127\.0\.0\.1:\d+/open#[A-Z2-7]{26}$`).MatchString(printed) {
		t.Fatalf("stdout %q", printed)
	}
	select {
	case target := <-current.opened:
		t.Fatalf("--no-browser opened %q", target)
	default:
	}
}
