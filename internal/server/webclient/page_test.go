package webclient_test

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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
	const page = `<p nonce="__NYLZO_CSP_NONCE__"></p>`
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

func TestCompressedPagesCarryTheirNonce(t *testing.T) {
	t.Parallel()
	long := `<!doctype html><script nonce="__NYLZO_CSP_NONCE__"></script>` + strings.Repeat("<p>nylzo</p>", 20000) +
		`<style nonce="__NYLZO_CSP_NONCE__"></style>__NYLZO_CSP_NONCE____NYLZO_CSP_NONCE__`
	files := builtClient(t)
	files["dist/index.html"] = &fstest.MapFile{Data: []byte(long)}
	pages, err := load(t, files).Render("https://nylzo.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/cs/", "/sent"} {
		page := pages.For(path)
		plain, packed := page.Plain("first"), page.Gzip("second")
		unpacked := gunzipped(t, packed)
		if strings.ReplaceAll(unpacked, "second", "N") != strings.ReplaceAll(string(plain), "first", "N") ||
			strings.Contains(unpacked, "__NYLZO_CSP_NONCE__") {
			t.Fatalf("%s: the compressed page differs from the plain one", path)
		}
	}
	sent := pages.For("/sent")
	packed := sent.Gzip("N0NCE")
	if len(packed) > len(sent.Plain("N0NCE"))/50 || strings.Count(gunzipped(t, packed), "N0NCE") != 4 {
		t.Fatalf("/sent: %d bytes compressed, or a nonce is missing", len(packed))
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
	got := load(t, builtClient(t)).Manifest("v1.2.3", "https://nylzo.example")
	want := webclient.Manifest{
		Build:            buildinfo.Info{Version: "v1.2.3"},
		Server:           "v1.2.3",
		Built:            true,
		NoncePlaceholder: "__NYLZO_CSP_NONCE__",
		PublicURL:        "https://nylzo.example",
		Pages:            []string{"/", "/cs/", "/sent", "/report", "/transparency", "/m/{id}"},
		Files: []webclient.ManifestFile{
			{Path: "/", SHA256: sha(builtHome("en"))},
			{Path: "/cs/", SHA256: sha(builtHome("cs"))},
			{Path: "/sent", SHA256: sha(builtIndex)},
			{Path: "/assets/favicon-d4.svg", SHA256: sha(faviconSVG)},
			{Path: "/assets/favicon-e5.png", SHA256: sha(faviconPNG)},
			{Path: "/assets/index-a1.js", SHA256: sha(appJS)},
			{Path: "/assets/kdf-worker-c3.js", SHA256: sha("self.onmessage=null")},
			{Path: "/assets/style-b2.css", SHA256: sha("body{}")},
		},
	}
	if gotJSON, wantJSON := encode(t, got), encode(t, want); gotJSON != wantJSON {
		t.Fatalf("manifest\n got %s\nwant %s", gotJSON, wantJSON)
	}
	placeholder := load(t, fstest.MapFS{"placeholder.html": {Data: []byte("placeholder")}}).Manifest("v1", "")
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
