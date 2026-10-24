package server_test

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/misaon/nylzo/internal/server"
)

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

func pageNonce(t *testing.T, csp string) string {
	t.Helper()
	start := strings.Index(csp, "'nonce-") + len("'nonce-")
	return csp[start : start+strings.Index(csp[start:], "'")]
}

func publicServer(t *testing.T, files fstest.MapFS, publicURL string) string {
	t.Helper()
	cfg := baseConfig(pepperA, nil)
	cfg.PublicURL = publicURL
	ts, err := newWebServerWith(t, files, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return ts.URL
}

func TestHomePagesHaveTheirOwnAddresses(t *testing.T) {
	t.Parallel()
	url := publicServer(t, builtClient(t), "")
	for path, want := range map[string]string{"/": "<h1>home en</h1>", "/cs/": "<h1>home cs</h1>", "/sent": "<body></body>", "/report": "<body></body>"} {
		resp, body := get(t, url+path, nil)
		if resp.StatusCode != 200 || !strings.Contains(body, want) {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
		if robots := resp.Header.Get("X-Robots-Tag"); (path == "/" || path == "/cs/") != (robots == "") {
			t.Fatalf("%s: X-Robots-Tag %q, only the home pages may be indexed (D82)", path, robots)
		}
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for path, location := range map[string]string{"/cs": "/cs/", "/cs?utm=1": "/cs/"} {
		resp, err := noFollow.Get(url + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 308 || resp.Header.Get("Location") != location || resp.Header.Get("X-Robots-Tag") != "noindex, nofollow" {
			t.Fatalf("%s: %d %q %v", path, resp.StatusCode, resp.Header.Get("Location"), resp.Header)
		}
	}
	for _, path := range []string{"/en/", "/cs/sent", "/cs/x", "/CS/"} {
		if resp, _ := get(t, url+path, nil); resp.StatusCode != 404 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
}

func TestPublicURLFillsTheHomePages(t *testing.T) {
	t.Parallel()
	with := publicServer(t, builtClient(t), "https://nylzo.example")
	without := publicServer(t, builtClient(t), "")
	for _, path := range []string{"/", "/cs/"} {
		_, body := get(t, with+path, nil)
		if !strings.Contains(body, `<link rel="canonical" href="https://nylzo.example/">`) || !strings.Contains(body, `content="https://nylzo.example/assets/favicon-e5.png"`) {
			t.Fatalf("%s with a public URL: %s", path, body)
		}
		_, plain := get(t, without+path, nil)
		if strings.Contains(plain, "canonical") || strings.Contains(plain, "og:image") {
			t.Fatalf("%s without a public URL keeps the absolute parts: %s", path, plain)
		}
		for _, b := range []string{body, plain} {
			if strings.Contains(b, "nylzo:public") || strings.Contains(b, "__NYLZO_PUBLIC_URL__") {
				t.Fatalf("%s: markers or placeholder left in the page: %s", path, b)
			}
		}
	}
}

func TestPagesFollowAcceptEncoding(t *testing.T) {
	t.Parallel()
	long := `<!doctype html><script nonce="__NYLZO_CSP_NONCE__"></script>` + strings.Repeat("<p>nylzo</p>", 20000) + `<style nonce="__NYLZO_CSP_NONCE__"></style>__NYLZO_CSP_NONCE____NYLZO_CSP_NONCE__`
	files := builtClient(t)
	files["web/dist/index.html"] = &fstest.MapFile{Data: []byte(long)}
	url := publicServer(t, files, "https://nylzo.example")
	for _, path := range []string{"/", "/cs/", "/sent"} {
		resp, plain := get(t, url+path, nil)
		zipped, packed := get(t, url+path, map[string]string{"Accept-Encoding": "gzip, deflate, br"})
		if resp.Header.Get("Content-Encoding") != "" || zipped.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("%s: encodings %q %q", path, resp.Header.Get("Content-Encoding"), zipped.Header.Get("Content-Encoding"))
		}
		if resp.Header.Get("Vary") != "Accept-Encoding" || zipped.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s: Vary %q %q", path, resp.Header.Get("Vary"), zipped.Header.Get("Vary"))
		}
		first, second := pageNonce(t, resp.Header.Get("Content-Security-Policy")), pageNonce(t, zipped.Header.Get("Content-Security-Policy"))
		unpacked := gunzipped(t, packed)
		if first == second || strings.ReplaceAll(unpacked, second, "N") != strings.ReplaceAll(plain, first, "N") || strings.Contains(unpacked, "__NYLZO_CSP_NONCE__") {
			t.Fatalf("%s: the compressed page differs from the plain one", path)
		}
		if path == "/sent" && (len(packed) > len(plain)/50 || strings.Count(unpacked, second) != 4) {
			t.Fatalf("/sent: %d bytes compressed from %d, %d nonces", len(packed), len(plain), strings.Count(unpacked, second))
		}
	}
	_, body := get(t, url+"/sent", map[string]string{"Accept-Encoding": "gzip;q=0, br"})
	if !strings.HasPrefix(body, "<!doctype html>") {
		t.Fatalf("gzip;q=0 must get the plain page")
	}
}

func TestRobotsAndSitemap(t *testing.T) {
	t.Parallel()
	without := publicServer(t, builtClient(t), "")
	resp, body := get(t, without+"/robots.txt", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" || body != "User-agent: *\nDisallow: /api/\n" {
		t.Fatalf("robots.txt: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if resp, _ := get(t, without+"/sitemap.xml", nil); resp.StatusCode != 404 {
		t.Fatalf("sitemap.xml without a public URL: %d", resp.StatusCode)
	}
	with := publicServer(t, builtClient(t), "https://nylzo.example:8443")
	if _, body := get(t, with+"/robots.txt", nil); body != "User-agent: *\nDisallow: /api/\n\nSitemap: https://nylzo.example:8443/sitemap.xml\n" {
		t.Fatalf("robots.txt with a public URL: %q", body)
	}
	resp, body = get(t, with+"/sitemap.xml", nil)
	links := `<xhtml:link rel="alternate" hreflang="en" href="https://nylzo.example:8443/"/><xhtml:link rel="alternate" hreflang="cs" href="https://nylzo.example:8443/cs/"/><xhtml:link rel="alternate" hreflang="x-default" href="https://nylzo.example:8443/"/>`
	want := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n" +
		`<url><loc>https://nylzo.example:8443/</loc>` + links + "</url>\n" +
		`<url><loc>https://nylzo.example:8443/cs/</loc>` + links + "</url>\n</urlset>\n"
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/xml; charset=utf-8" || body != want {
		t.Fatalf("sitemap.xml: %d %q\n%s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}

func TestCheckPublicURL(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{
		"https://nylzo.example", "https://nylzo.example:8443", "https://xn--nylzo-9ua.example", "https://192.0.2.1",
		"https://[2001:db8::1]:443", "http://localhost:8080", "http://127.0.0.1:8096", "http://[::1]:8080", "http://localhost",
	} {
		if err := server.CheckPublicURL(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "nylzo.example", "//nylzo.example", "https://", "https://nylzo.example/", "https://nylzo.example/cs/", "https://Nylzo.example",
		"HTTPS://nylzo.example", "http://nylzo.example", "http://localhost.evil.example", "ftp://nylzo.example", "https://nylzo.example:",
		"https://nylzo.example:0", "https://nylzo.example:65536", "https://nylzo.example:08443", "https://user@nylzo.example",
		"https://nylzo.example?x=1", "https://nylzo.example?", "https://nylzo.example#top", "https://[fe80::1%25en0]", "https://nylzo_example.com",
		"https://-nylzo.example", "https://nylzo-.example", `https://ny"lzo.example`, "https://ny<lzo.example", "https://nylzo.example\n",
		"https://" + strings.Repeat("a", 64) + ".example", "https:nylzo.example",
	} {
		if err := server.CheckPublicURL(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

func TestPublicURLFromEnv(t *testing.T) {
	t.Parallel()
	env := func(publicURL string) func(string) string {
		return func(key string) string {
			if key == "NYLZO_PUBLIC_URL" {
				return publicURL
			}
			return ""
		}
	}
	cfg, err := server.FromEnv(env(" https://nylzo.example "), false)
	if err != nil || cfg.PublicURL != "https://nylzo.example" {
		t.Fatalf("%q %v", cfg.PublicURL, err)
	}
	if _, err := server.FromEnv(env("https://nylzo.example/"), false); !errors.Is(err, server.ErrConfig) || !strings.Contains(err.Error(), "NYLZO_PUBLIC_URL") {
		t.Fatalf("a public URL with a path must stop the server: %v", err)
	}
	cfg = baseConfig(pepperA, nil)
	cfg.PublicURL = "http://nylzo.example"
	if err := cfg.Validate(); !errors.Is(err, server.ErrConfig) {
		t.Fatalf("Validate must refuse plain http to a public host: %v", err)
	}
}

func TestRenderPage(t *testing.T) {
	t.Parallel()
	template := "a<!--nylzo:public-->[__NYLZO_PUBLIC_URL__/x]<!--/nylzo:public-->b<!--nylzo:public-->__NYLZO_PUBLIC_URL__<!--/nylzo:public-->c"
	for publicURL, want := range map[string]string{"": "abc", "https://n.example": "a[https://n.example/x]bhttps://n.examplec"} {
		if got, err := server.RenderPage(template, publicURL); err != nil || got != want {
			t.Fatalf("%q: %q %v", publicURL, got, err)
		}
	}
	if got, err := server.RenderPage("plain", "https://n.example"); err != nil || got != "plain" {
		t.Fatalf("a page without markers: %q %v", got, err)
	}
}
