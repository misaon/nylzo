package server_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
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

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/webclient"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	builtIndex = `<!doctype html><html><head><link rel="icon" href="/assets/favicon-d4.svg" type="image/svg+xml"><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body></body></html>`
	appJS      = "console.log('nylzo')"
)

func builtHome(language string) string {
	return `<!doctype html><html lang="` + language + `"><head><title>home ` + language + `</title>
<!--nylzo:public-->
<link rel="canonical" href="__NYLZO_PUBLIC_URL__/"><meta property="og:image" content="__NYLZO_PUBLIC_URL__/assets/favicon-e5.png">
<!--/nylzo:public-->
<script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body><h1>home ` + language + `</h1></body></html>`
}

func gzipped(t *testing.T, data string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func gunzipped(t *testing.T, body string) string {
	t.Helper()
	reader, err := gzip.NewReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(plain)
}

func builtClient(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"placeholder.html":             {Data: []byte("placeholder")},
		"dist/index.html":              {Data: []byte(builtIndex)},
		"dist/home.en.html":            {Data: []byte(builtHome("en"))},
		"dist/home.cs.html":            {Data: []byte(builtHome("cs"))},
		"dist/assets/index-a1.js":      {Data: []byte(appJS)},
		"dist/assets/index-a1.js.gz":   {Data: gzipped(t, appJS)},
		"dist/assets/style-b2.css":     {Data: []byte("body{}")},
		"dist/assets/kdf-worker-c3.js": {Data: []byte("self.onmessage=null")},
		"dist/assets/favicon-d4.svg":   {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		"dist/assets/favicon-e5.png":   {Data: []byte("\x89PNG\r\n\x1a\n")},
	}
}

func newWebServer(t *testing.T, files fs.FS, logs io.Writer) (*httptest.Server, error) {
	t.Helper()
	return newWebServerWith(t, files, logs, baseConfig(pepperA, nil))
}

func newWebServerWith(t *testing.T, files fs.FS, logs io.Writer, cfg config.Server) (*httptest.Server, error) {
	t.Helper()
	dir := t.TempDir()
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
	srv, err := server.New(
		cfg,
		st,
		blobs,
		server.WithWebFiles(files),
		server.WithLogger(slog.New(slog.NewJSONHandler(logs, nil))),
	)
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
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
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

func pageNonce(csp string) string {
	start := strings.Index(csp, "'nonce-") + len("'nonce-")
	return csp[start : start+strings.Index(csp[start:], "'")]
}

func TestBuiltClientIsServed(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	ts, err := newWebServer(t, builtClient(t), &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"msg":"web-client","built":true,"assets":5`) {
		t.Fatalf("the server did not log the embedded client: %s", logs.String())
	}
	for _, path := range []string{"/", "/cs/", "/sent", "/report", "/transparency", "/m/" + core.NewID().String()} {
		resp, body := get(t, ts.URL+path, nil)
		nonce := pageNonce(resp.Header.Get("Content-Security-Policy"))
		if resp.StatusCode != 200 || strings.Contains(body, "placeholder") ||
			strings.Contains(body, "__NYLZO_CSP_NONCE__") || strings.Count(body, `nonce="`+nonce+`"`) != 2 {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	for path, contentType := range map[string]string{
		"/assets/index-a1.js":      "text/javascript; charset=utf-8",
		"/assets/style-b2.css":     "text/css; charset=utf-8",
		"/assets/kdf-worker-c3.js": "text/javascript; charset=utf-8",
		"/assets/favicon-d4.svg":   "image/svg+xml",
		"/assets/favicon-e5.png":   "image/png",
	} {
		resp, body := get(t, ts.URL+path, nil)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != contentType || body == "" {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if csp := resp.Header.Get("Content-Security-Policy"); csp != webclient.NonHTMLCSP {
			t.Fatalf("%s: the worker and the other assets must keep CSP default-src 'none', got %q", path, csp)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Cache-Control") != "no-store" ||
			resp.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s: headers %v", path, resp.Header)
		}
	}
	for _, path := range []string{
		"/assets/missing.js", "/assets/", "/assets/../placeholder.html", "/assets/%2e%2e%2fplaceholder.html",
		"/assets/index-a1.js.br", "/assets/index-a1.js.gz", "/index.html",
	} {
		if resp, _ := get(t, ts.URL+path, nil); resp.StatusCode != 404 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
	resp, body := get(t, ts.URL+"/assets/index-a1.js", map[string]string{"Accept-Encoding": "gzip, deflate, br"})
	if resp.Header.Get("Content-Encoding") != "gzip" || gunzipped(t, body) != appJS {
		t.Fatalf("a gzip variant was not sent to a client that accepts it: %q", resp.Header.Get("Content-Encoding"))
	}
	resp, body = get(t, ts.URL+"/assets/style-b2.css", map[string]string{"Accept-Encoding": "br, gzip"})
	if resp.Header.Get("Content-Encoding") != "" || body != "body{}" {
		t.Fatalf("an asset without compressed variants: %q %q", resp.Header.Get("Content-Encoding"), body)
	}
}

func TestHomePagesHaveTheirOwnAddresses(t *testing.T) {
	t.Parallel()
	ts, err := newWebServer(t, builtClient(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/":       "<h1>home en</h1>",
		"/cs/":    "<h1>home cs</h1>",
		"/sent":   "<body></body>",
		"/report": "<body></body>",
	} {
		resp, body := get(t, ts.URL+path, nil)
		if resp.StatusCode != 200 || !strings.Contains(body, want) {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
		if robots := resp.Header.Get("X-Robots-Tag"); (path == "/" || path == "/cs/") != (robots == "") {
			t.Fatalf("%s: X-Robots-Tag %q, only the home pages may be indexed (D82)", path, robots)
		}
	}
	noFollow := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, path := range []string{"/cs", "/cs?utm=1"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+path, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 308 || resp.Header.Get("Location") != "/cs/" ||
			resp.Header.Get("X-Robots-Tag") != "noindex, nofollow" {
			t.Fatalf("%s: %d %q %v", path, resp.StatusCode, resp.Header.Get("Location"), resp.Header)
		}
	}
	for _, path := range []string{"/en/", "/cs/sent", "/cs/x", "/CS/"} {
		if resp, _ := get(t, ts.URL+path, nil); resp.StatusCode != 404 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
}

func TestPagesAreCompressedWithAFreshNonce(t *testing.T) {
	t.Parallel()
	ts, err := newWebServer(t, builtClient(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/sent"} {
		resp, plain := get(t, ts.URL+path, nil)
		zipped, packed := get(t, ts.URL+path, map[string]string{"Accept-Encoding": "gzip, deflate, br"})
		if resp.Header.Get("Content-Encoding") != "" || zipped.Header.Get("Content-Encoding") != "gzip" ||
			resp.Header.Get("Vary") != "Accept-Encoding" || zipped.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s: encoding headers %v %v", path, resp.Header, zipped.Header)
		}
		first := pageNonce(resp.Header.Get("Content-Security-Policy"))
		second := pageNonce(zipped.Header.Get("Content-Security-Policy"))
		unpacked := gunzipped(t, packed)
		if first == second || strings.ReplaceAll(unpacked, second, "N") != strings.ReplaceAll(plain, first, "N") {
			t.Fatalf("%s: the compressed page differs from the plain one or reuses its nonce", path)
		}
	}
}

func TestRobotsSitemapAndManifest(t *testing.T) {
	t.Parallel()
	without, err := newWebServer(t, builtClient(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, body := get(t, without.URL+"/robots.txt", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" ||
		body != string(webclient.Robots("")) {
		t.Fatalf("robots.txt: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if resp, _ := get(t, without.URL+"/sitemap.xml", nil); resp.StatusCode != 404 {
		t.Fatalf("sitemap.xml without a public URL: %d", resp.StatusCode)
	}
	cfg := baseConfig(pepperA, nil)
	cfg.PublicURL = "https://nylzo.example"
	cfg.Version = "v1.2.3"
	with, err := newWebServerWith(t, builtClient(t), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, body = get(t, with.URL+"/sitemap.xml", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/xml; charset=utf-8" ||
		body != string(webclient.Sitemap(cfg.PublicURL)) {
		t.Fatalf("sitemap.xml: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp, body = get(t, with.URL+webclient.ManifestPath, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json; charset=utf-8" ||
		resp.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP ||
		resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("manifest: %d %v", resp.StatusCode, resp.Header)
	}
	build, err := webclient.Load(builtClient(t))
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(build.Manifest("v1.2.3", cfg.PublicURL))
	if err != nil {
		t.Fatal(err)
	}
	if body != string(want) {
		t.Fatalf("manifest\n got %s\nwant %s", body, want)
	}
}

func TestWithoutABuildTheServerServesThePlaceholder(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	placeholder := fstest.MapFS{"placeholder.html": {Data: []byte(`<p nonce="__NYLZO_CSP_NONCE__">placeholder</p>`)}}
	ts, err := newWebServer(t, placeholder, &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"msg":"web-client","built":false,"assets":0`) {
		t.Fatalf("the server did not log that it serves the placeholder: %s", logs.String())
	}
	resp, body := get(t, ts.URL+"/", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "placeholder") ||
		strings.Contains(body, "__NYLZO_CSP_NONCE__") {
		t.Fatalf("placeholder: %d %s", resp.StatusCode, body)
	}
	if resp, _ := get(t, ts.URL+"/assets/index-a1.js", nil); resp.StatusCode != 404 {
		t.Fatalf("an asset without a build: %d", resp.StatusCode)
	}
	embedded, err := newWebServer(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := get(t, embedded.URL+"/", nil); resp.StatusCode != 200 || !strings.Contains(body, "<html") {
		t.Fatalf("embedded client: %d %s", resp.StatusCode, body)
	}
	broken := builtClient(t)
	maps.DeleteFunc(broken, func(name string, _ *fstest.MapFile) bool { return name == "dist/home.cs.html" })
	if _, err := newWebServer(t, broken, nil); err == nil {
		t.Fatal("the server started with a broken build")
	}
}
