package cli

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/server/webclient"
)

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

func builtWebClient() (webclient.Build, error) {
	files := fstest.MapFS{"dist/index.html": {Data: []byte(`<!doctype html><head></head><body></body>`)}}
	for _, name := range webclient.HomeTemplates() {
		files["dist/"+name] = &fstest.MapFile{Data: []byte(`<!doctype html><head></head><body></body>`)}
	}
	return webclient.Load(files)
}

func unbuiltWebClient() (webclient.Build, error) { return webclient.Build{}, nil }

type localRun struct {
	code   int
	stdout string
	stderr string
}

func runLocal(
	t *testing.T,
	run func(context.Context, *command.Environment, []string) int,
	stdin string,
	webClient func() (webclient.Build, error),
	arguments ...string,
) localRun {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stdout, stderr lockedBuffer
	environment := &command.Environment{
		Stdin:     strings.NewReader(stdin),
		Stdout:    &stdout,
		Stderr:    &stderr,
		Getenv:    getenvFrom(nil),
		Terminal:  noTerminal{},
		Version:   "test",
		WebClient: webClient,
		Browser:   func(string) error { return nil },
	}
	done := make(chan int, 1)
	go func() { done <- run(ctx, environment, arguments) }()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		select {
		case code := <-done:
			return localRun{code, stdout.String(), stderr.String()}
		case <-time.After(10 * time.Millisecond):
		}
		if stdout.String() != "" {
			cancel()
		}
	}
	t.Fatalf("%v did not end: %s", arguments, stderr.String())
	return localRun{}
}

func TestWebAndOpenServeTheVerifiedClientUntilStopped(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "handed over")
	web := runLocal(t, runWeb, "", builtWebClient, "--no-browser", "--server", instance.url)
	if web.code != command.ExitOK || !strings.HasPrefix(web.stdout, "http://") {
		t.Fatalf("web: %+v", web)
	}
	open := runLocal(t, runOpen, link, builtWebClient, "--no-browser", "--server", instance.url)
	_, fragment, _ := strings.Cut(link, "#")
	if open.code != command.ExitError || !strings.Contains(open.stdout, "/open#") ||
		strings.Contains(open.stdout+open.stderr, fragment) {
		t.Fatalf("open stopped before a browser took the link: %+v", open)
	}
}

func TestWebAndOpenRefuseWithoutAClientOrAPublicInstance(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "never handed over")
	for name, run := range map[string]func(context.Context, *command.Environment, []string) int{
		"web":  runWeb,
		"open": runOpen,
	} {
		for _, test := range []struct {
			server    string
			webClient func() (webclient.Build, error)
			want      string
		}{
			{"https://[fe80::1%25eth0]", builtWebClient, "the instance address"},
			{instance.url, unbuiltWebClient, command.ErrNoLocalClient.Error()},
			{"ftp://nylzo.example", builtWebClient, client.ErrServerURL.Error()},
		} {
			result := runLocal(t, run, link, test.webClient, "--no-browser", "--server", test.server)
			if result.code != command.ExitError || !strings.Contains(result.stderr, test.want) || result.stdout != "" {
				t.Errorf("%s with %s: %+v", name, test.server, result)
			}
		}
	}
}
