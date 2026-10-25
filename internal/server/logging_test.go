package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
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
	if n := len(
		lines,
	); n < 2 || !strings.Contains(lines[n-2], `"dropped":20`) ||
		!strings.Contains(lines[n-1], `"dropped":0`) {
		t.Fatalf("dropped lines must be reported once and then reset: %v", lines[max(0, len(lines)-2):])
	}
}

func TestIgnoredForwardingHeaderIsLoggedOncePerHourWithoutAddresses(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.TrustedProxies = nil
	h := newHarness(t, cfg)
	h.statusFrom(request{method: "GET", path: "/api/v1/instance"})
	if strings.Contains(h.logBuf.String(), "forwarded-header-ignored") {
		t.Fatal("a request without a forwarding header must not warn")
	}
	for range 3 {
		h.statusFrom(request{method: "GET", path: "/api/v1/instance", ip: "198.51.100.7"})
	}
	if n := strings.Count(h.logBuf.String(), `"msg":"forwarded-header-ignored","header":"X-Forwarded-For"`); n != 1 {
		t.Fatalf("want one warning in the hour, got %d: %s", n, h.logBuf.String())
	}
	h.clock.Advance(time.Hour)
	h.statusFrom(request{method: "GET", path: "/", headers: map[string]string{"Forwarded": "for=198.51.100.7"}})
	logs := h.logBuf.String()
	if strings.Count(logs, "forwarded-header-ignored") != 2 || !strings.Contains(logs, `"header":"Forwarded"`) {
		t.Fatalf("the next hour must warn again, for pages too: %s", logs)
	}
	if strings.Contains(logs, "198.51.100") {
		t.Fatal("the log names a client address")
	}
}

func TestUsedForwardingHeadersDoNotWarn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	h.statusFrom(request{method: "GET", path: "/api/v1/instance", ip: "198.51.100.7"})
	scaleway := baseConfig(pepperA, nil)
	scaleway.ProxyMode = server.ProxyModeScaleway
	s := newHarness(t, scaleway)
	s.statusFrom(
		request{
			method:  "GET",
			path:    "/api/v1/instance",
			headers: map[string]string{"X-Real-IP": "198.51.100.7", "X-Forwarded-For": "203.0.113.1"},
		},
	)
	for _, logs := range []string{h.logBuf.String(), s.logBuf.String()} {
		if strings.Contains(logs, "forwarded-header-ignored") {
			t.Fatalf("a header the server used must not warn: %s", logs)
		}
	}
}

func TestForwardingHeadersThatLeaveTheProxyAddressWarn(t *testing.T) {
	t.Parallel()
	scaleway := baseConfig(pepperA, nil)
	scaleway.ProxyMode = server.ProxyModeScaleway
	for _, tc := range []struct {
		name    string
		cfg     server.Config
		headers map[string]string
		header  string
	}{
		{"scaleway without X-Real-IP", scaleway, map[string]string{"X-Forwarded-For": "203.0.113.1"}, "X-Forwarded-For"},
		{"scaleway with an unusable X-Real-IP", scaleway, map[string]string{"X-Real-IP": "not an address"}, "X-Real-IP"},
		{"trusted proxy with only X-Real-IP", baseConfig(pepperA, nil), map[string]string{"X-Real-IP": "203.0.113.1"}, "X-Real-IP"},
		{"trusted proxy with only Forwarded", baseConfig(pepperA, nil), map[string]string{"Forwarded": "for=203.0.113.1"}, "Forwarded"},
		{"trusted proxy with an unreadable X-Forwarded-For", baseConfig(pepperA, nil), map[string]string{"X-Forwarded-For": "not an address"}, "X-Forwarded-For"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, tc.cfg)
			h.statusFrom(request{method: "GET", path: "/api/v1/instance", headers: tc.headers})
			if !strings.Contains(h.logBuf.String(), `"msg":"forwarded-header-ignored","header":"`+tc.header+`"`) {
				t.Fatalf("no warning: %s", h.logBuf.String())
			}
		})
	}
}

func TestForwardingWarningRecoversFromAClockThatJumpedAhead(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.TrustedProxies = nil
	h := newHarness(t, cfg)
	h.clock.Advance(48 * time.Hour)
	h.statusFrom(request{method: "GET", path: "/", headers: map[string]string{"X-Forwarded-For": "203.0.113.1"}})
	h.clock.Advance(-47 * time.Hour)
	h.statusFrom(request{method: "GET", path: "/", headers: map[string]string{"X-Forwarded-For": "203.0.113.1"}})
	if n := strings.Count(h.logBuf.String(), "forwarded-header-ignored"); n != 2 {
		t.Fatalf("want a warning after the clock stepped back, got %d", n)
	}
}
