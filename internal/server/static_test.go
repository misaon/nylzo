package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const builtIndex = `<!doctype html><html><head><meta property="csp-nonce" nonce="__NYLZO_CSP_NONCE__"><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body></body></html>`

func builtClient() fstest.MapFS {
	return fstest.MapFS{
		"web/placeholder.html":             {Data: []byte("placeholder")},
		"web/dist/index.html":              {Data: []byte(builtIndex)},
		"web/dist/assets/index-a1.js":      {Data: []byte("console.log(1)")},
		"web/dist/assets/style-b2.css":     {Data: []byte("body{}")},
		"web/dist/assets/kdf-worker-c3.js": {Data: []byte("self.onmessage=null")},
	}
}

func newWebServer(t *testing.T, files fstest.MapFS) (*httptest.Server, error) {
	t.Helper()
	dir := t.TempDir()
	cfg := baseConfig(pepperA, nil)
	cfg.DataDir = dir
	st, err := sqlite.Open(filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(cfg, st, blobs, server.WithWebFiles(files))
	if err != nil {
		return nil, err
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, nil
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func TestBuiltClientIsServed(t *testing.T) {
	ts, err := newWebServer(t, builtClient())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/m/" + core.NewID().String()} {
		resp, body := get(t, ts.URL+path)
		csp := resp.Header.Get("Content-Security-Policy")
		start := strings.Index(csp, "'nonce-") + len("'nonce-")
		nonce := csp[start : start+strings.Index(csp[start:], "'")]
		if resp.StatusCode != 200 || strings.Contains(body, "placeholder") || strings.Contains(body, "__NYLZO_CSP_NONCE__") || strings.Count(body, `nonce="`+nonce+`"`) != 3 {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	assets := map[string]string{
		"/assets/index-a1.js":      "text/javascript; charset=utf-8",
		"/assets/style-b2.css":     "text/css; charset=utf-8",
		"/assets/kdf-worker-c3.js": "text/javascript; charset=utf-8",
	}
	for path, contentType := range assets {
		resp, body := get(t, ts.URL+path)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != contentType || body == "" {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if csp := resp.Header.Get("Content-Security-Policy"); csp != "default-src 'none'; frame-ancestors 'none'" {
			t.Fatalf("%s: the worker and the other assets must keep CSP default-src 'none', got %q", path, csp)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: security headers missing", path)
		}
	}
	for _, path := range []string{"/assets/missing.js", "/assets/", "/assets/../web/placeholder.html", "/index.html"} {
		if resp, _ := get(t, ts.URL+path); resp.StatusCode != 404 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
}

func TestPlaceholderWithoutBuild(t *testing.T) {
	ts, err := newWebServer(t, fstest.MapFS{"web/placeholder.html": {Data: []byte(`<p nonce="__NYLZO_CSP_NONCE__">placeholder</p>`)}})
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := get(t, ts.URL+"/"); resp.StatusCode != 200 || !strings.Contains(body, "placeholder") || strings.Contains(body, "__NYLZO_CSP_NONCE__") {
		t.Fatalf("placeholder: %d %s", resp.StatusCode, body)
	}
	if resp, _ := get(t, ts.URL+"/assets/index-a1.js"); resp.StatusCode != 404 {
		t.Fatalf("an asset without a build: %d", resp.StatusCode)
	}
}

func TestUnknownAssetTypeRefusesToStart(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"unknown extension": {"web/placeholder.html": {}, "web/dist/index.html": {}, "web/dist/assets/logo.svg": {Data: []byte("<svg/>")}},
		"nested directory":  {"web/placeholder.html": {}, "web/dist/index.html": {}, "web/dist/assets/sub/a.js": {}},
	} {
		if _, err := newWebServer(t, files); err == nil {
			t.Fatalf("%s: the server started", name)
		}
	}
}
