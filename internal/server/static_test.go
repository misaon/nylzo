package server_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"log/slog"
	"maps"
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

const builtIndex = `<!doctype html><html><head><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body></body></html>`

const appJS = "console.log('nylzo')"

func gzipped(t *testing.T, data string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func builtClient(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"web/placeholder.html":             {Data: []byte("placeholder")},
		"web/dist/index.html":              {Data: []byte(builtIndex)},
		"web/dist/assets/index-a1.js":      {Data: []byte(appJS)},
		"web/dist/assets/index-a1.js.br":   {Data: []byte("brotli bytes")},
		"web/dist/assets/index-a1.js.gz":   {Data: gzipped(t, appJS)},
		"web/dist/assets/style-b2.css":     {Data: []byte("body{}")},
		"web/dist/assets/kdf-worker-c3.js": {Data: []byte("self.onmessage=null")},
	}
}

func newWebServer(t *testing.T, files fs.FS, logs io.Writer) (*httptest.Server, error) {
	t.Helper()
	dir := t.TempDir()
	cfg := baseConfig(pepperA, nil)
	cfg.DataDir = dir
	st, err := sqlite.Open(t.Context(), filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if logs == nil {
		logs = io.Discard
	}
	srv, err := server.New(cfg, st, blobs, server.WithWebFiles(files), server.WithLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	if err != nil {
		return nil, err
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, nil
}

var rawClient = &http.Client{Transport: &http.Transport{DisableCompression: true}}

func get(t *testing.T, url string, header map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := rawClient.Do(req)
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
	t.Parallel()
	var logs bytes.Buffer
	ts, err := newWebServer(t, builtClient(t), &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"msg":"web-client","built":true,"assets":3`) {
		t.Fatalf("the server did not log the embedded client: %s", logs.String())
	}
	for _, path := range []string{"/", "/m/" + core.NewID().String()} {
		resp, body := get(t, ts.URL+path, nil)
		csp := resp.Header.Get("Content-Security-Policy")
		start := strings.Index(csp, "'nonce-") + len("'nonce-")
		nonce := csp[start : start+strings.Index(csp[start:], "'")]
		if resp.StatusCode != 200 || strings.Contains(body, "placeholder") || strings.Contains(body, "__NYLZO_CSP_NONCE__") || strings.Count(body, `nonce="`+nonce+`"`) != 2 {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	assets := map[string]string{
		"/assets/index-a1.js":      "text/javascript; charset=utf-8",
		"/assets/style-b2.css":     "text/css; charset=utf-8",
		"/assets/kdf-worker-c3.js": "text/javascript; charset=utf-8",
	}
	for path, contentType := range assets {
		resp, body := get(t, ts.URL+path, nil)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != contentType || body == "" {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if csp := resp.Header.Get("Content-Security-Policy"); csp != "default-src 'none'; frame-ancestors 'none'" {
			t.Fatalf("%s: the worker and the other assets must keep CSP default-src 'none', got %q", path, csp)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s: headers %v", path, resp.Header)
		}
	}
	for _, path := range []string{
		"/assets/missing.js", "/assets/", "/assets/../web/placeholder.html", "/assets/%2e%2e%2fweb%2fplaceholder.html",
		"/assets/index-a1.js.br", "/assets/index-a1.js.gz", "/index.html",
	} {
		if resp, _ := get(t, ts.URL+path, nil); resp.StatusCode != 404 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
}

func TestAssetsFollowAcceptEncoding(t *testing.T) {
	t.Parallel()
	files := builtClient(t)
	ts, err := newWebServer(t, files, nil)
	if err != nil {
		t.Fatal(err)
	}
	gz := string(files["web/dist/assets/index-a1.js.gz"].Data)
	for _, c := range []struct {
		accept, encoding, body string
	}{
		{"", "", appJS},
		{"identity", "", appJS},
		{"br", "br", "brotli bytes"},
		{"gzip", "gzip", gz},
		{"gzip, deflate, br", "br", "brotli bytes"},
		{"br;q=0, gzip", "gzip", gz},
		{"BR;Q=0.5", "br", "brotli bytes"},
		{"*", "br", "brotli bytes"},
		{"*;q=0", "", appJS},
		{"gzip;q=0, br;q=0", "", appJS},
		{"*, br;q=0", "gzip", gz},
	} {
		resp, body := get(t, ts.URL+"/assets/index-a1.js", map[string]string{"Accept-Encoding": c.accept})
		if resp.Header.Get("Content-Encoding") != c.encoding || body != c.body || resp.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("Accept-Encoding %q: got %q %q", c.accept, resp.Header.Get("Content-Encoding"), body)
		}
	}
	resp, body := get(t, ts.URL+"/assets/style-b2.css", map[string]string{"Accept-Encoding": "br, gzip"})
	if resp.Header.Get("Content-Encoding") != "" || body != "body{}" {
		t.Fatalf("an asset without compressed variants: %q %q", resp.Header.Get("Content-Encoding"), body)
	}
}

func TestPlaceholderWithoutBuild(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	ts, err := newWebServer(t, fstest.MapFS{"web/placeholder.html": {Data: []byte(`<p nonce="__NYLZO_CSP_NONCE__">placeholder</p>`)}}, &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"msg":"web-client","built":false,"assets":0`) {
		t.Fatalf("the server did not log that it serves the placeholder: %s", logs.String())
	}
	if resp, body := get(t, ts.URL+"/", nil); resp.StatusCode != 200 || !strings.Contains(body, "placeholder") || strings.Contains(body, "__NYLZO_CSP_NONCE__") {
		t.Fatalf("placeholder: %d %s", resp.StatusCode, body)
	}
	if resp, _ := get(t, ts.URL+"/assets/index-a1.js", nil); resp.StatusCode != 404 {
		t.Fatalf("an asset without a build: %d", resp.StatusCode)
	}
}

func TestBuildWithoutAssetsStarts(t *testing.T) {
	t.Parallel()
	ts, err := newWebServer(t, fstest.MapFS{"web/dist/index.html": {Data: []byte(`<p nonce="__NYLZO_CSP_NONCE__">built</p>`)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := get(t, ts.URL+"/", nil); resp.StatusCode != 200 || !strings.Contains(body, "built") {
		t.Fatalf("a build without assets: %d %s", resp.StatusCode, body)
	}
}

func TestNoWebFilesMeansTheEmbeddedClient(t *testing.T) {
	t.Parallel()
	ts, err := newWebServer(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := get(t, ts.URL+"/", nil); resp.StatusCode != 200 || !strings.Contains(body, "<html") {
		t.Fatalf("embedded client: %d %s", resp.StatusCode, body)
	}
}

func TestBrokenBuildRefusesToStart(t *testing.T) {
	t.Parallel()
	index := func(extra fstest.MapFS) fstest.MapFS {
		files := fstest.MapFS{"web/dist/index.html": {Data: []byte(`<p nonce="__NYLZO_CSP_NONCE__"></p>`)}}
		maps.Copy(files, extra)
		return files
	}
	for name, files := range map[string]fstest.MapFS{
		"unknown extension":                 index(fstest.MapFS{"web/dist/assets/logo.svg": {Data: []byte("<svg/>")}}),
		"nested directory":                  index(fstest.MapFS{"web/dist/assets/sub/a.js": {}}),
		"directory named like a script":     index(fstest.MapFS{"web/dist/assets/x.js/a.js": {}}),
		"compressed variant without a file": index(fstest.MapFS{"web/dist/assets/a.js.br": {Data: []byte("x")}}),
		"gzip that is not the file":         index(fstest.MapFS{"web/dist/assets/a.js": {Data: []byte("a")}, "web/dist/assets/a.js.gz": {Data: gzipped(t, "b")}}),
		"gzip that is not gzip":             index(fstest.MapFS{"web/dist/assets/a.js": {Data: []byte("a")}, "web/dist/assets/a.js.gz": {Data: []byte("a")}}),
		"compressed unknown type":           index(fstest.MapFS{"web/dist/assets/a.svg": {}, "web/dist/assets/a.svg.br": {}}),
		"index without the nonce":           {"web/dist/index.html": {Data: []byte(`<script src="/assets/a.js"></script>`)}, "web/dist/assets/a.js": {}},
		"index with a missing asset": {
			"web/dist/index.html":  {Data: []byte(`<script nonce="__NYLZO_CSP_NONCE__" src="/assets/a.js"></script><link href="/assets/gone.css">`)},
			"web/dist/assets/a.js": {},
		},
	} {
		if _, err := newWebServer(t, files, nil); err == nil {
			t.Fatalf("%s: the server started", name)
		}
	}
}
