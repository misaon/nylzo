package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

func TestServerLogsHideAddressesAndSecrets(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	srv := h.srv.HTTPServer()
	if srv.ErrorLog == nil {
		t.Fatal("ErrorLog is nil: net/http would log peer addresses to stderr")
	}
	srv.ErrorLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
	s := core.NewSecret()
	id := core.NewID()
	h.do(request{method: "GET", path: "/m/" + id.String() + "%231.0." + core.Encode(s[:])})
	logs := h.logBuf.String()
	if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, core.Encode(s[:])) {
		t.Fatal("log contains a peer address or a link secret")
	}
	if !strings.Contains(logs, "net/http error") {
		t.Fatal("net/http errors must still leave a signal")
	}
}

func TestHTTPErrorLogCategoriesAndLimit(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	errLog := h.srv.HTTPServer().ErrorLog
	errLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
	for range 40 {
		errLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	}
	errLog.Printf("http2: panic serving 203.0.113.99:4242: boom")
	logs := h.logBuf.String()
	for _, want := range []string{`"category":"tls-handshake"`, `"category":"http2"`, `"category":"panic"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("%s missing: %s", want, logs)
		}
	}
	if n := strings.Count(logs, `"level":"WARN","msg":"net/http error"`); n > 25 {
		t.Fatalf("net/http warnings are not rate limited: %d", n)
	}
	if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, "boom") {
		t.Fatal("net/http error text reached the log")
	}
}

func TestHTTPErrorLogReportsDrops(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	errLog := h.srv.HTTPServer().ErrorLog
	for range 41 {
		errLog.Printf("http2: server: error reading preface")
	}
	h.clock.Advance(time.Second)
	errLog.Printf("http2: server: error reading preface")
	if !strings.Contains(h.logBuf.String(), `"dropped":21`) {
		t.Fatalf("drop count not reported: %s", h.logBuf.String())
	}
}

func TestHTTPErrorLogReportsAndResetsDroppedLines(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	errLog := h.srv.HTTPServer().ErrorLog
	for range 40 {
		errLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	}
	h.clock.Advance(time.Second)
	errLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	h.clock.Advance(time.Second)
	errLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	lines := strings.Split(strings.TrimSpace(h.logBuf.String()), "\n")
	if n := len(lines); n < 2 || !strings.Contains(lines[n-2], `"dropped":20`) || !strings.Contains(lines[n-1], `"dropped":0`) {
		t.Fatalf("dropped lines must be reported once and then reset: %v", lines[max(0, len(lines)-2):])
	}
}
