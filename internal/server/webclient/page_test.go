package webclient_test

import (
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/server/webclient"
)

func gunzipped(t *testing.T, body []byte) string {
	t.Helper()
	reader, err := gzip.NewReader(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(plain)
}

func TestPublicURLFillsTheHomePages(t *testing.T) {
	t.Parallel()
	build := load(t, builtClient(t))
	for _, path := range []string{"/", "/cs/"} {
		with, err := build.Page(path, "https://nylzo.example")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(with, `<link rel="canonical" href="https://nylzo.example/">`) ||
			!strings.Contains(with, `content="https://nylzo.example/assets/favicon-e5.png"`) {
			t.Fatalf("%s with a public URL: %s", path, with)
		}
		without, err := build.Page(path, "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(without, "canonical") || strings.Contains(without, "og:image") {
			t.Fatalf("%s without a public URL keeps the absolute parts: %s", path, without)
		}
		for _, page := range []string{with, without} {
			if strings.Contains(page, "nylzo:public") || strings.Contains(page, "__NYLZO_PUBLIC_URL__") {
				t.Fatalf("%s: markers or placeholder left in the page: %s", path, page)
			}
		}
	}
	page, err := build.Page("/sent", "https://nylzo.example")
	if err != nil || page != builtIndex {
		t.Fatalf("the other pages use the index template: %q %v", page, err)
	}
}

func TestPublicBlocksAreFilledOrDropped(t *testing.T) {
	t.Parallel()
	const page = `<p></p>`
	const blocks = page + "a<!--nylzo:public-->[__NYLZO_PUBLIC_URL__/x]<!--/nylzo:public-->b" +
		"<!--nylzo:public-->__NYLZO_PUBLIC_URL__<!--/nylzo:public-->c"
	build := load(t, templates(page, blocks, page))
	for publicURL, want := range map[string]string{"": page + "abc", "https://n.example": page + "a[https://n.example/x]bhttps://n.examplec"} {
		if got, err := build.Page("/", publicURL); err != nil || got != want {
			t.Fatalf("%q: %q %v", publicURL, got, err)
		}
	}
	if got, err := build.Page("/sent", "https://n.example"); err != nil || got != page {
		t.Fatalf("a page without markers: %q %v", got, err)
	}
}

func hashSource(content string) string {
	sum := sha512.Sum384([]byte(content))
	return "'sha384-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestPagePolicyAdmitsOnlyTheInlineCodeOfThePage(t *testing.T) {
	t.Parallel()
	const page = `<!doctype html><head><script type="importmap">{"integrity":{}}</script>` +
		`<script type="application/ld+json">{"name":"Nylzo"}</script><script type="module" src="/assets/index-a1.js"></script>` +
		`<style>p{}</style><noscript><style>body{}</style></noscript><style>p{}</style></head>` + "\n<body></body>"
	files := builtClient(t)
	files["dist/index.html"] = &fstest.MapFile{Data: []byte(page + strings.Repeat("<p>nylzo</p>", 20000))}
	pages, err := load(t, files).Render("https://nylzo.example")
	if err != nil {
		t.Fatal(err)
	}
	sent := pages.For("/sent")
	want := "default-src 'none'; script-src 'self' " + hashSource(`{"integrity":{}}`) + "; style-src 'self' " +
		hashSource("p{}") + " " + hashSource("body{}") + "; connect-src 'self'"
	if !strings.HasPrefix(sent.Policy(), want) {
		t.Fatalf("policy %q, want it to start with %q", sent.Policy(), want)
	}
	if home := pages.For("/").Policy(); !strings.Contains(home, "script-src 'self'; style-src 'self';") {
		t.Fatalf("a page without inline code must admit none: %q", home)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/sent", http.NoBody)
	request.Header.Set("Accept-Encoding", "gzip")
	sent.Serve(recorder, request)
	packed := recorder.Body.Bytes()
	if gunzipped(t, packed) != string(sent.Body()) || len(packed) > len(sent.Body())/50 ||
		recorder.Header().Get("Content-Security-Policy") != sent.Policy() {
		t.Fatalf("/sent: %d bytes compressed, or the compressed page differs from the plain one", len(packed))
	}
}

func TestRobotsAndSitemap(t *testing.T) {
	t.Parallel()
	if robots := string(webclient.Robots("")); robots != "User-agent: *\nDisallow: /api/\n" {
		t.Fatalf("robots.txt without a public URL: %q", robots)
	}
	if webclient.Sitemap("") != nil {
		t.Fatal("a sitemap without a public URL")
	}
	const publicURL = "https://nylzo.example:8443"
	if robots := string(
		webclient.Robots(publicURL),
	); robots != "User-agent: *\nDisallow: /api/\n\nSitemap: "+publicURL+"/sitemap.xml\n" {
		t.Fatalf("robots.txt with a public URL: %q", robots)
	}
	links := `<xhtml:link rel="alternate" hreflang="en" href="https://nylzo.example:8443/"/><xhtml:link rel="alternate" hreflang="cs" href="https://nylzo.example:8443/cs/"/><xhtml:link rel="alternate" hreflang="x-default" href="https://nylzo.example:8443/"/>`
	want := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n" +
		`<url><loc>https://nylzo.example:8443/</loc>` + links + "</url>\n" +
		`<url><loc>https://nylzo.example:8443/cs/</loc>` + links + "</url>\n</urlset>\n"
	if sitemap := string(webclient.Sitemap(publicURL)); sitemap != want {
		t.Fatalf("sitemap.xml:\n%s", sitemap)
	}
}

func TestCheckPublicURL(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{
		"https://nylzo.example", "https://nylzo.example:8443", "https://xn--nylzo-9ua.example", "https://192.0.2.1",
		"https://[2001:db8::1]:443", "http://localhost:8080", "http://127.0.0.1:8096", "http://[::1]:8080",
		"http://localhost",
	} {
		if err := webclient.CheckPublicURL(valid); err != nil {
			t.Errorf("%q: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"", "nylzo.example", "//nylzo.example", "https://", "https://nylzo.example/", "https://nylzo.example/cs/",
		"https://Nylzo.example", "HTTPS://nylzo.example", "http://nylzo.example", "http://localhost.evil.example",
		"ftp://nylzo.example", "https://nylzo.example:", "https://nylzo.example:0", "https://nylzo.example:65536",
		"https://nylzo.example:08443", "https://user@nylzo.example", "https://nylzo.example?x=1",
		"https://nylzo.example?", "https://nylzo.example#top", "https://[fe80::1%25en0]", "https://nylzo_example.com",
		"https://-nylzo.example", "https://nylzo-.example", `https://ny"lzo.example`, "https://ny<lzo.example",
		"https://nylzo.example\n", "https://" + strings.Repeat("a", 64) + ".example", "https:nylzo.example",
	} {
		if err := webclient.CheckPublicURL(invalid); err == nil {
			t.Errorf("%q passed", invalid)
		}
	}
}

func TestManifestListsTheEmbeddedFiles(t *testing.T) {
	t.Parallel()
	build := load(t, builtClient(t))
	pages, err := build.Render("https://nylzo.example")
	if err != nil {
		t.Fatal(err)
	}
	served := func(path string) string {
		page, renderErr := build.Page(path, "https://nylzo.example")
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		return sha(page)
	}
	got := build.Manifest("v1.2.3", "https://nylzo.example", pages, buildinfo.Info{})
	want := webclient.Manifest{
		Build:     buildinfo.Info{Version: "v1.2.3"},
		Server:    "v1.2.3",
		Built:     true,
		PublicURL: "https://nylzo.example",
		Pages:     []string{"/", "/cs/", "/sent", "/report", "/transparency", "/r", "/m/{id}"},
		Files: []webclient.ManifestFile{
			{Path: "/", SHA256: served("/")},
			{Path: "/cs/", SHA256: served("/cs/")},
			{Path: "/sent", SHA256: served("/sent")},
			{Path: "/assets/favicon-d4.svg", SHA256: sha(faviconSVG)},
			{Path: "/assets/favicon-e5.png", SHA256: sha(faviconPNG)},
			{Path: "/assets/index-a1.js", SHA256: sha(appJS)},
			{Path: "/assets/kdf-worker-c3.js", SHA256: sha("self.onmessage=null")},
			{Path: "/assets/style-b2.css", SHA256: sha("body{}")},
		},
		Templates: []webclient.ManifestTemplate{
			{Name: "home.cs.html", SHA256: sha(builtHome("cs"))},
			{Name: "home.en.html", SHA256: sha(builtHome("en"))},
			{Name: "index.html", SHA256: sha(builtIndex)},
		},
	}
	if gotJSON, wantJSON := encode(t, got), encode(t, want); gotJSON != wantJSON {
		t.Fatalf("manifest\n got %s\nwant %s", gotJSON, wantJSON)
	}
	placeholderBuild := load(t, fstest.MapFS{"placeholder.html": {Data: []byte("placeholder")}})
	placeholderPages, err := placeholderBuild.Render("")
	if err != nil {
		t.Fatal(err)
	}
	placeholder := placeholderBuild.Manifest("v1", "", placeholderPages, buildinfo.Info{})
	if encoded := encode(t, placeholder); !strings.Contains(encoded, `"built":false`) ||
		!strings.Contains(encoded, `"public_url":""`) || !strings.Contains(encoded, `"files":[]`) {
		t.Fatalf("manifest without a build: %s", encoded)
	}
}

func sha(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func encode(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
