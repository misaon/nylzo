package webclient_test

import (
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
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

func rendered(t *testing.T, build webclient.Build, path, publicURL string) string {
	t.Helper()
	pages, err := build.Render(publicURL, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(pages.For(path).Body())
}

func TestPublicURLFillsTheHomePages(t *testing.T) {
	t.Parallel()
	build := load(t, builtClient(t))
	for _, path := range []string{"/", "/cs/", "/pt-br/", "/zh-hans/"} {
		with := rendered(t, build, path, "https://nylzo.example")
		if !strings.Contains(with, `<link rel="canonical" href="https://nylzo.example/">`) ||
			!strings.Contains(with, `content="https://nylzo.example/assets/favicon-e5.png"`) {
			t.Fatalf("%s with a public URL: %s", path, with)
		}
		without := rendered(t, build, path, "")
		if strings.Contains(without, "canonical") || strings.Contains(without, "og:image") {
			t.Fatalf("%s without a public URL keeps the absolute parts: %s", path, without)
		}
		for _, page := range []string{with, without} {
			if strings.Contains(page, "nylzo:public") || strings.Contains(page, "__NYLZO_PUBLIC_URL__") {
				t.Fatalf("%s: markers or placeholder left in the page: %s", path, page)
			}
		}
	}
	if page := rendered(t, build, "/sent", "https://nylzo.example"); page != builtIndex {
		t.Fatalf("the other pages use the index template: %q", page)
	}
}

func TestPublicBlocksAreFilledOrDropped(t *testing.T) {
	t.Parallel()
	const page = `<p></p>`
	const blocks = page + "a<!--nylzo:public-->[__NYLZO_PUBLIC_URL__/x]<!--/nylzo:public-->b" +
		"<!--nylzo:public-->__NYLZO_PUBLIC_URL__<!--/nylzo:public-->c"
	build := load(t, templates(page, func(language string) string {
		if language == "en" {
			return blocks
		}
		return page
	}))
	for publicURL, want := range map[string]string{"": page + "abc", "https://n.example": page + "a[https://n.example/x]bhttps://n.examplec"} {
		if got := rendered(t, build, "/", publicURL); got != want {
			t.Fatalf("%q: %q", publicURL, got)
		}
	}
	if got := rendered(t, build, "/sent", "https://n.example"); got != page {
		t.Fatalf("a page without markers: %q", got)
	}
	for _, unsafe := range []string{`https://n.example"><script>alert(1)</script>`, "https://n.example'&", "https://n<x>.example"} {
		if _, err := build.Render(unsafe, ""); err == nil {
			t.Fatalf("%q was rendered", unsafe)
		}
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
	pages, err := load(t, files).Render("https://nylzo.example", "")
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
	var links strings.Builder
	for _, language := range homeLanguages {
		links.WriteString(
			`<xhtml:link rel="alternate" hreflang="` + language + `" href="` + publicURL + homePath(language) + `"/>`,
		)
	}
	links.WriteString(`<xhtml:link rel="alternate" hreflang="x-default" href="` + publicURL + `/"/>`)
	var want strings.Builder
	want.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	want.WriteString(
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n",
	)
	for _, language := range homeLanguages {
		want.WriteString(`<url><loc>` + publicURL + homePath(language) + `</loc>` + links.String() + "</url>\n")
	}
	want.WriteString("</urlset>\n")
	if sitemap := string(webclient.Sitemap(publicURL)); sitemap != want.String() {
		t.Fatalf("sitemap.xml:\n%s", sitemap)
	}
}

func TestConnectionAllowlistAdmitsOnlyTheOperatorContact(t *testing.T) {
	t.Parallel()
	const linked = `response-origin "https://github.com/misaon/nylzo" "https://github.com/misaon/nylzo/*"`
	for contact, want := range map[string]string{
		"":                                    "(" + linked + ")",
		"Security+Desk_1@Mail.Example.org":    "(" + linked + ` "mailto\\:Security\\+Desk_1@Mail.Example.org?#")`,
		"https://Example.org:8443/contact":    "(" + linked + ` "https://example.org:8443/contact?#")`,
		"https://example.org:443":             "(" + linked + ` "https://example.org/?#")`,
		"https://example.org/a!b:c_d?q=1+2&x": "(" + linked + ` "https://example.org/a!b\\:c_d?q=1\\+2&x#")`,
	} {
		if got := webclient.ConnectionAllowlist(contact); got != want {
			t.Errorf("%q: %s, want %s", contact, got, want)
		}
	}
	build := load(t, builtClient(t))
	pages, err := build.Render("https://nylzo.example", "security@example.org")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	pages.For("/transparency").Serve(response, httptest.NewRequest(http.MethodGet, "/transparency", http.NoBody))
	if got := response.Header().
		Get("Connection-Allowlist"); got != webclient.ConnectionAllowlist(
		"security@example.org",
	) {
		t.Fatalf("the transparency page carries %q", got)
	}
	for _, unsafe := range []string{
		"javascript:alert(1)", "http://example.org/", "mailto:a@b.example", `https://example.org/"`,
		"https://user@example.org/", "https://example.org/#top", "https://example.org/a(b)", "https://example.org:0/",
		"https://example.org:99999/", "https://exa_mple.org/", "https://example.org/%zz", "https://example.org/?q=%zz",
		"https://example.org/a/../b", "https://example.org/?", "https://example.org:/",
	} {
		if _, renderErr := build.Render("", unsafe); renderErr == nil {
			t.Errorf("%q was accepted as a contact", unsafe)
		}
	}
}

func TestAPageTheBuildLacksStillCarriesTheStrictestPolicy(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	webclient.Pages{}.For("/").Serve(response, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if got := response.Header().Get("Content-Security-Policy"); got != webclient.NonHTMLCSP {
		t.Errorf("Content-Security-Policy %q, want %q", got, webclient.NonHTMLCSP)
	}
	if got := response.Header().Get("Connection-Allowlist"); got != "(response-origin)" {
		t.Errorf("Connection-Allowlist %q", got)
	}
}

func TestManifestListsTheEmbeddedFiles(t *testing.T) {
	t.Parallel()
	build := load(t, builtClient(t))
	pages, err := build.Render("https://nylzo.example", "")
	if err != nil {
		t.Fatal(err)
	}
	served := func(path string) string {
		return sha(rendered(t, build, path, "https://nylzo.example"))
	}
	got := build.Manifest("v1.2.3", "https://nylzo.example", pages, buildinfo.Info{})
	var homePaths []string
	var homeFiles []webclient.ManifestFile
	var homeTemplates []webclient.ManifestTemplate
	for _, language := range homeLanguages {
		homePaths = append(homePaths, homePath(language))
		homeFiles = append(
			homeFiles,
			webclient.ManifestFile{Path: homePath(language), SHA256: served(homePath(language))},
		)
		homeTemplates = append(homeTemplates, webclient.ManifestTemplate{
			Name:   "home." + language + ".html",
			SHA256: sha(builtHome(language)),
		})
	}
	slices.SortFunc(homeTemplates, func(first, second webclient.ManifestTemplate) int {
		return strings.Compare(first.Name, second.Name)
	})
	want := webclient.Manifest{
		Build:     buildinfo.Info{Version: "v1.2.3"},
		Server:    "v1.2.3",
		Built:     true,
		PublicURL: "https://nylzo.example",
		Pages:     append(homePaths, "/sent", "/transparency", "/terms", "/agents", "/r", "/m/{id}"),
		Files: append(homeFiles,
			webclient.ManifestFile{Path: "/sent", SHA256: served("/sent")},
			webclient.ManifestFile{Path: "/assets/favicon-d4.svg", SHA256: sha(faviconSVG)},
			webclient.ManifestFile{Path: "/assets/favicon-e5.png", SHA256: sha(faviconPNG)},
			webclient.ManifestFile{Path: "/assets/index-a1.js", SHA256: sha(appJS)},
			webclient.ManifestFile{Path: "/assets/kdf-worker-c3.js", SHA256: sha("self.onmessage=null")},
			webclient.ManifestFile{Path: "/assets/style-b2.css", SHA256: sha("body{}")},
		),
		Templates: append(homeTemplates, webclient.ManifestTemplate{Name: "index.html", SHA256: sha(builtIndex)}),
	}
	if gotJSON, wantJSON := encode(t, got), encode(t, want); gotJSON != wantJSON {
		t.Fatalf("manifest\n got %s\nwant %s", gotJSON, wantJSON)
	}
	placeholderBuild := load(t, fstest.MapFS{"placeholder.html": {Data: []byte("placeholder")}})
	placeholderPages, err := placeholderBuild.Render("", "")
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

const analyticsPatterns = ` "https://static.cloudflareinsights.com/beacon.min.js?#" "https://cloudflareinsights.com/cdn-cgi/rum?#")`

func withAnalytics(allowlist string) string {
	return strings.TrimSuffix(allowlist, ")") + analyticsPatterns
}

func directives(policy string) map[string]string {
	parsed := map[string]string{}
	for directive := range strings.SplitSeq(policy, "; ") {
		name, value, _ := strings.Cut(directive, " ")
		parsed[name] = value
	}
	return parsed
}

func expectNoAnalytics(t *testing.T, where string, page webclient.Page, contact string) {
	t.Helper()
	if strings.Contains(page.Policy(), "cloudflare") || strings.Contains(page.Policy(), "nylzo-analytics") ||
		page.Allowlist() != webclient.ConnectionAllowlist(contact) ||
		!maps.Equal(page.SecurityHeaders("/"), webclient.SecurityHeaders("/")) {
		t.Errorf("%s loads analytics: %q %q", where, page.Policy(), page.Allowlist())
	}
}

func TestOnlyTheHomePagesOfNylzoComLoadWebAnalytics(t *testing.T) {
	t.Parallel()
	const contact = "security@example.org"
	build := load(t, builtClient(t))
	shared := webclient.SecurityHeaders("/")
	analyticsHeaders := maps.Clone(shared)
	analyticsHeaders["Permissions-Policy"] = strings.Replace(
		shared["Permissions-Policy"], "ch-ua-high-entropy-values=()", "ch-ua-high-entropy-values=(self)", 1,
	)
	delete(analyticsHeaders, "Integrity-Policy")
	for _, publicURL := range []string{"https://nylzo.com", "https://nylzo.com:443"} {
		pages, err := build.Render(publicURL, contact)
		if err != nil {
			t.Fatal(err)
		}
		homes := 0
		for name, page := range pages {
			if !strings.HasPrefix(name, "home.") {
				expectNoAnalytics(t, publicURL+" "+name, page, contact)
				continue
			}
			homes++
			policy := directives(page.Policy())
			if !strings.HasSuffix(policy["script-src"], " https://static.cloudflareinsights.com/beacon.min.js") ||
				policy["connect-src"] != "'self' https://cloudflareinsights.com/cdn-cgi/rum" ||
				policy["trusted-types"] != "nylzo-kdf nylzo-analytics" {
				t.Errorf("%s %s policy %q", publicURL, name, page.Policy())
			}
			if page.Allowlist() != withAnalytics(webclient.ConnectionAllowlist(contact)) ||
				page.ContactlessAllowlist() != withAnalytics(webclient.ConnectionAllowlist("")) {
				t.Errorf("%s %s allowlist %q", publicURL, name, page.Allowlist())
			}
			if !maps.Equal(page.SecurityHeaders("/"), analyticsHeaders) {
				t.Errorf("%s %s headers %v", publicURL, name, page.SecurityHeaders("/"))
			}
			response := httptest.NewRecorder()
			webclient.SetSecurityHeaders(response.Header(), true)
			page.Serve(response, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
			served := response.Header()
			if served.Get("Content-Security-Policy") != page.Policy() ||
				served.Get("Connection-Allowlist") != page.Allowlist() ||
				served.Get("Permissions-Policy") != analyticsHeaders["Permissions-Policy"] ||
				served.Get("Integrity-Policy") != "" {
				t.Errorf("%s %s serves other headers: %v", publicURL, name, served)
			}
		}
		if homes < 12 {
			t.Fatalf("%s: checked %d home pages, want every language", publicURL, homes)
		}
	}
	local, err := build.RenderLocal("https://nylzo.com", contact)
	if err != nil {
		t.Fatal(err)
	}
	other, err := build.Render("https://nylzo.example", contact)
	if err != nil {
		t.Fatal(err)
	}
	for client, pages := range map[string]webclient.Pages{"the local client": local, "another instance": other} {
		for name, page := range pages {
			expectNoAnalytics(t, client+" "+name, page, contact)
		}
	}
}
