package server

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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/webclient"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	builtIndex   = `<!doctype html><html><head><link rel="icon" href="/assets/favicon-d4.svg" type="image/svg+xml"><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body></body></html>`
	appJS        = "console.log('nylzo')"
	previewAgent = "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"
)

var requiredHeaders = map[string]string{
	"Referrer-Policy":              "no-referrer",
	"Cache-Control":                "no-store",
	"X-Content-Type-Options":       "nosniff",
	"Cross-Origin-Opener-Policy":   "same-origin",
	"Cross-Origin-Resource-Policy": "same-origin",
	"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	"Permissions-Policy":           "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
}

func htmlCSPFromSpec(t *testing.T) string {
	t.Helper()
	spec, err := os.ReadFile("../../spec/protocol.md")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "- HTML responses carry `Content-Security-Policy: "
	_, rest, ok := strings.Cut(string(spec), marker)
	csp, _, closed := strings.Cut(rest, "`")
	if !ok || !closed || strings.Count(csp, "{nonce}") != 2 {
		t.Fatal("spec section 9 lost the HTML CSP line")
	}
	return csp
}

func checkSecurityHeaders(t *testing.T, name string, response testResponse, indexable bool) {
	t.Helper()
	for header, want := range requiredHeaders {
		if got := response.Header.Get(header); got != want {
			t.Fatalf("%s: header %s = %q", name, header, got)
		}
	}
	if robots := response.Header.Get("X-Robots-Tag"); indexable != (robots == "") ||
		(!indexable && robots != "noindex, nofollow") {
		t.Fatalf("%s: X-Robots-Tag = %q, only the home pages may be indexed (D80, D82)", name, robots)
	}
	if csp := response.Header.Get("Content-Security-Policy"); !strings.HasPrefix(
		response.Header.Get("Content-Type"),
		"text/html",
	) && csp != "default-src 'none'; frame-ancestors 'none'" {
		t.Fatalf("%s: a response other than HTML carries CSP %q", name, csp)
	}
}

func TestSecurityHeadersAndNonce(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	for _, path := range []string{
		"/", "/cs/", "/sent", "/report", "/m/" + core.NewID().String(), "/robots.txt", "/api/v1/instance",
		"/api/v1/messages/" + core.NewID().String(), "/healthz", "/nope",
	} {
		response, _ := harness.do(&testRequest{method: http.MethodGet, path: path})
		checkSecurityHeaders(t, path, response, path == "/" || path == "/cs/")
	}
	prefetch := map[string]string{"Sec-Purpose": "prefetch"}
	for _, testCase := range []struct {
		request   *testRequest
		status    int
		indexable bool
	}{
		{&testRequest{method: http.MethodHead, path: "/"}, http.StatusOK, true},
		{&testRequest{method: http.MethodPost, path: "/"}, http.StatusMethodNotAllowed, false},
		{&testRequest{method: http.MethodGet, path: "/api/v1/instance", headers: prefetch}, http.StatusServiceUnavailable, false},
		{
			&testRequest{method: http.MethodGet, path: "/m/" + core.NewID().String(), headers: prefetch},
			http.StatusServiceUnavailable,
			false,
		},
		{&testRequest{method: http.MethodPost, path: "/api/v1/messages", body: []byte("x")}, http.StatusBadRequest, false},
		{&testRequest{method: http.MethodGet, path: "/m/not-an-id"}, http.StatusNotFound, false},
	} {
		name := testCase.request.method + " " + testCase.request.path
		response, _ := harness.do(testCase.request)
		if response.StatusCode != testCase.status {
			t.Fatalf("%s: status %d, want %d", name, response.StatusCode, testCase.status)
		}
		checkSecurityHeaders(t, name, response, testCase.indexable)
	}
	response, body := harness.do(&testRequest{
		method:  http.MethodGet,
		path:    "/api/v1/messages/not-an-id",
		headers: map[string]string{"Authorization": core.AuthScheme + " short"},
	})
	if response.StatusCode != http.StatusNotFound || string(body) != `{"error":"not_found"}` {
		t.Fatalf("malformed API id: %d %s", response.StatusCode, body)
	}

	settings := baseSettings(pepperA, nil)
	settings.Rates.BucketPerSecond = 1
	settings.Rates.BucketBurst = 2
	throttled := newHarness(t, settings)
	instance := &testRequest{method: http.MethodGet, path: "/api/v1/instance"}
	throttled.do(instance)
	throttled.do(instance)
	if response, _ = throttled.do(instance); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("throttled: status %d", response.StatusCode)
	}
	checkSecurityHeaders(t, "throttled", response, false)

	specCSP := htmlCSPFromSpec(t)
	nonces := map[string]bool{}
	pages := []string{"/", "/cs/", "/sent", "/report", "/m/" + core.NewID().String()}
	for i := range 2 * len(pages) {
		response, body = harness.do(&testRequest{method: http.MethodGet, path: pages[i%len(pages)]})
		csp := response.Header.Get("Content-Security-Policy")
		nonce := pageNonce(csp)
		if len(nonce) < 22 || csp != strings.ReplaceAll(specCSP, "{nonce}", nonce) {
			t.Fatalf("csp %q differs from spec section 9 %q", csp, specCSP)
		}
		if nonces[nonce] {
			t.Fatal("nonce repeated")
		}
		nonces[nonce] = true
		if !bytes.Contains(body, []byte(`nonce="`+nonce+`"`)) || bytes.Contains(body, []byte("__NYLZO_CSP_NONCE__")) {
			t.Fatal("nonce not injected into the page")
		}
	}

	response, preview := harness.do(&testRequest{
		method:  http.MethodGet,
		path:    "/m/" + core.NewID().String(),
		headers: map[string]string{"User-Agent": previewAgent},
	})
	if response.StatusCode != http.StatusOK || bytes.Contains(preview, []byte("<script")) ||
		bytes.Contains(preview, []byte("<style")) || !bytes.Contains(preview, []byte(`name="viewport"`)) ||
		!bytes.Contains(preview, []byte(`name="robots" content="noindex, nofollow"`)) {
		t.Fatalf("preview agent page: %d %s", response.StatusCode, preview)
	}
	client, err := EmbeddedWebClient()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/cs/", "/sent"} {
		if _, page := harness.do(
			&testRequest{method: http.MethodGet, path: path},
		); client.Built && bytes.Contains(page, []byte(`name="robots"`)) {
			t.Fatalf("%s: the built client HTML must leave robots to the X-Robots-Tag header (D80)", path)
		}
	}
	nonceAttribute := regexp.MustCompile(`nonce="[^"]*"`)
	_, home := harness.do(&testRequest{method: http.MethodGet, path: "/"})
	_, previewHome := harness.do(&testRequest{
		method:  http.MethodGet,
		path:    "/",
		headers: map[string]string{"User-Agent": previewAgent},
	})
	if !bytes.Equal(nonceAttribute.ReplaceAll(previewHome, nil), nonceAttribute.ReplaceAll(home, nil)) {
		t.Fatalf("a preview agent must get the normal home page: %s", previewHome)
	}
}

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
	return newWebServerWith(t, files, logs, baseSettings(pepperA, nil))
}

func newWebServerWith(t *testing.T, files fs.FS, logs io.Writer, settings *config.Server) (*httptest.Server, error) {
	t.Helper()
	directory := t.TempDir()
	settings.DataDir = directory
	database, err := sqlite.Open(t.Context(), filepath.Join(directory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	blobs, err := disk.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	if logs == nil {
		logs = io.Discard
	}
	server, err := New(
		settings,
		database,
		blobs,
		WithWebFiles(files),
		WithLogger(slog.New(slog.NewJSONHandler(logs, nil))),
	)
	if err != nil {
		return nil, err
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer, nil
}

type fetchedPage struct {
	testResponse

	body string
}

var (
	rawClient        = &http.Client{Transport: &http.Transport{DisableCompression: true}}
	noRedirectClient = &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
)

func get(t *testing.T, url string, headers map[string]string) fetchedPage {
	t.Helper()
	return fetch(t, rawClient, url, headers)
}

func fetch(t *testing.T, client *http.Client, url string, headers map[string]string) fetchedPage {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return fetchedPage{
		StatusCode: response.StatusCode,
		Header:     response.Header,
		Close:      response.Close,
		body:       string(body),
	}
}

func pageNonce(csp string) string {
	_, rest, _ := strings.Cut(csp, "'nonce-")
	nonce, _, _ := strings.Cut(rest, "'")
	return nonce
}

func TestBuiltClientIsServed(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	httpServer, err := newWebServer(t, builtClient(t), &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"msg":"web-client","built":true,"assets":5`) {
		t.Fatalf("the server did not log the embedded client: %s", logs.String())
	}
	for _, path := range []string{"/", "/cs/", "/sent", "/report", "/transparency", "/m/" + core.NewID().String()} {
		page := get(t, httpServer.URL+path, nil)
		nonce := pageNonce(page.Header.Get("Content-Security-Policy"))
		if page.StatusCode != http.StatusOK || strings.Contains(page.body, "placeholder") ||
			strings.Contains(page.body, "__NYLZO_CSP_NONCE__") || strings.Count(page.body, `nonce="`+nonce+`"`) != 2 {
			t.Fatalf("%s: %d %s", path, page.StatusCode, page.body)
		}
	}
	for path, contentType := range map[string]string{
		"/assets/index-a1.js":      "text/javascript; charset=utf-8",
		"/assets/style-b2.css":     "text/css; charset=utf-8",
		"/assets/kdf-worker-c3.js": "text/javascript; charset=utf-8",
		"/assets/favicon-d4.svg":   "image/svg+xml",
		"/assets/favicon-e5.png":   "image/png",
	} {
		asset := get(t, httpServer.URL+path, nil)
		if asset.StatusCode != http.StatusOK || asset.Header.Get("Content-Type") != contentType || asset.body == "" {
			t.Fatalf("%s: %d %s", path, asset.StatusCode, asset.Header.Get("Content-Type"))
		}
		if csp := asset.Header.Get("Content-Security-Policy"); csp != webclient.NonHTMLCSP {
			t.Fatalf("%s: the worker and the other assets must keep CSP default-src 'none', got %q", path, csp)
		}
		if asset.Header.Get("X-Content-Type-Options") != "nosniff" ||
			asset.Header.Get("Cache-Control") != "no-store" ||
			asset.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s: headers %v", path, asset.Header)
		}
	}
	for _, path := range []string{
		"/assets/missing.js", "/assets/", "/assets/../placeholder.html", "/assets/%2e%2e%2fplaceholder.html",
		"/assets/index-a1.js.br", "/assets/index-a1.js.gz", "/index.html",
	} {
		if missing := get(t, httpServer.URL+path, nil); missing.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", path, missing.StatusCode)
		}
	}
	script := get(t, httpServer.URL+"/assets/index-a1.js", map[string]string{"Accept-Encoding": "gzip, deflate, br"})
	if script.Header.Get("Content-Encoding") != "gzip" || gunzipped(t, script.body) != appJS {
		t.Fatalf("a gzip variant was not sent to a client that accepts it: %q", script.Header.Get("Content-Encoding"))
	}
	style := get(t, httpServer.URL+"/assets/style-b2.css", map[string]string{"Accept-Encoding": "br, gzip"})
	if style.Header.Get("Content-Encoding") != "" || style.body != "body{}" {
		t.Fatalf("an asset without compressed variants: %q %q", style.Header.Get("Content-Encoding"), style.body)
	}
}

func TestHomePagesHaveTheirOwnAddresses(t *testing.T) {
	t.Parallel()
	httpServer, err := newWebServer(t, builtClient(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/":       "<h1>home en</h1>",
		"/cs/":    "<h1>home cs</h1>",
		"/sent":   "<body></body>",
		"/report": "<body></body>",
	} {
		page := get(t, httpServer.URL+path, nil)
		if page.StatusCode != http.StatusOK || !strings.Contains(page.body, want) {
			t.Fatalf("%s: %d %s", path, page.StatusCode, page.body)
		}
		if robots := page.Header.Get("X-Robots-Tag"); (path == "/" || path == "/cs/") != (robots == "") {
			t.Fatalf("%s: X-Robots-Tag %q, only the home pages may be indexed (D82)", path, robots)
		}
	}
	for _, path := range []string{"/cs", "/cs?utm=1"} {
		redirect := fetch(t, noRedirectClient, httpServer.URL+path, nil)
		if redirect.StatusCode != http.StatusPermanentRedirect || redirect.Header.Get("Location") != "/cs/" ||
			redirect.Header.Get("X-Robots-Tag") != "noindex, nofollow" {
			t.Fatalf("%s: %d %q %v", path, redirect.StatusCode, redirect.Header.Get("Location"), redirect.Header)
		}
	}
	for _, path := range []string{"/en/", "/cs/sent", "/cs/x", "/CS/"} {
		if missing := get(t, httpServer.URL+path, nil); missing.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", path, missing.StatusCode)
		}
	}
}

func TestPagesAreCompressedWithAFreshNonce(t *testing.T) {
	t.Parallel()
	httpServer, err := newWebServer(t, builtClient(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/sent"} {
		plain := get(t, httpServer.URL+path, nil)
		zipped := get(t, httpServer.URL+path, map[string]string{"Accept-Encoding": "gzip, deflate, br"})
		if plain.Header.Get("Content-Encoding") != "" || zipped.Header.Get("Content-Encoding") != "gzip" ||
			plain.Header.Get("Vary") != "Accept-Encoding" || zipped.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s: encoding headers %v %v", path, plain.Header, zipped.Header)
		}
		first := pageNonce(plain.Header.Get("Content-Security-Policy"))
		second := pageNonce(zipped.Header.Get("Content-Security-Policy"))
		unpacked := gunzipped(t, zipped.body)
		if first == second || strings.ReplaceAll(unpacked, second, "N") != strings.ReplaceAll(plain.body, first, "N") {
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
	robots := get(t, without.URL+"/robots.txt", nil)
	if robots.StatusCode != http.StatusOK || robots.Header.Get("Content-Type") != "text/plain; charset=utf-8" ||
		robots.body != string(webclient.Robots("")) {
		t.Fatalf("robots.txt: %d %q %q", robots.StatusCode, robots.Header.Get("Content-Type"), robots.body)
	}
	if sitemap := get(t, without.URL+"/sitemap.xml", nil); sitemap.StatusCode != http.StatusNotFound {
		t.Fatalf("sitemap.xml without a public URL: %d", sitemap.StatusCode)
	}
	settings := baseSettings(pepperA, nil)
	settings.PublicURL = "https://nylzo.example"
	settings.Version = "v1.2.3"
	with, err := newWebServerWith(t, builtClient(t), nil, settings)
	if err != nil {
		t.Fatal(err)
	}
	sitemap := get(t, with.URL+"/sitemap.xml", nil)
	if sitemap.StatusCode != http.StatusOK ||
		sitemap.Header.Get("Content-Type") != "application/xml; charset=utf-8" ||
		sitemap.body != string(webclient.Sitemap(settings.PublicURL)) {
		t.Fatalf("sitemap.xml: %d %q", sitemap.StatusCode, sitemap.Header.Get("Content-Type"))
	}
	manifest := get(t, with.URL+webclient.ManifestPath, nil)
	if manifest.StatusCode != http.StatusOK ||
		manifest.Header.Get("Content-Type") != "application/json; charset=utf-8" ||
		manifest.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP ||
		manifest.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("manifest: %d %v", manifest.StatusCode, manifest.Header)
	}
	build, err := webclient.Load(builtClient(t))
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(build.Manifest("v1.2.3", settings.PublicURL))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.body != string(want) {
		t.Fatalf("manifest\n got %s\nwant %s", manifest.body, want)
	}
}

func TestWithoutABuildTheServerServesThePlaceholder(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	placeholder := fstest.MapFS{"placeholder.html": {Data: []byte(`<p nonce="__NYLZO_CSP_NONCE__">placeholder</p>`)}}
	httpServer, err := newWebServer(t, placeholder, &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"msg":"web-client","built":false,"assets":0`) {
		t.Fatalf("the server did not log that it serves the placeholder: %s", logs.String())
	}
	page := get(t, httpServer.URL+"/", nil)
	if page.StatusCode != http.StatusOK || !strings.Contains(page.body, "placeholder") ||
		strings.Contains(page.body, "__NYLZO_CSP_NONCE__") {
		t.Fatalf("placeholder: %d %s", page.StatusCode, page.body)
	}
	if asset := get(t, httpServer.URL+"/assets/index-a1.js", nil); asset.StatusCode != http.StatusNotFound {
		t.Fatalf("an asset without a build: %d", asset.StatusCode)
	}
	embedded, err := newWebServer(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page = get(t, embedded.URL+"/", nil); page.StatusCode != http.StatusOK || !strings.Contains(page.body, "<html") {
		t.Fatalf("embedded client: %d %s", page.StatusCode, page.body)
	}
	broken := builtClient(t)
	maps.DeleteFunc(broken, func(name string, _ *fstest.MapFile) bool { return name == "dist/home.cs.html" })
	if _, brokenErr := newWebServer(t, broken, nil); brokenErr == nil {
		t.Fatal("the server started with a broken build")
	}
}

func TestTransparencyReportsWhatTheServerUses(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Version = "v1.2.3"
	settings.Rates.CreatePerHour = 7
	settings.Rates.GlobalDailyCreates = 900
	settings.PoWDifficulty = 5
	settings.Limits.MaxOpens = 4
	settings.Operator = config.Operator{
		Name:    "Example s.r.o.",
		Contact: "security@example.org",
		Hosting: "Vultr, Frankfurt",
	}
	httpServer, err := newWebServerWith(t, builtClient(t), nil, settings)
	if err != nil {
		t.Fatal(err)
	}
	report := get(t, httpServer.URL+TransparencyPath, nil)
	if report.StatusCode != http.StatusOK || report.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP {
		t.Fatalf("%d %v", report.StatusCode, report.Header)
	}
	for _, want := range []string{
		`"server":"v1.2.3"`,
		`"operator":{"name":"Example s.r.o.","contact":"security@example.org","hosting":"Vultr, Frankfurt"}`,
		`"max_opens":4`,
		`"create_per_hour":7`,
		`"global_daily_creates":900`,
		`"pow_difficulty":5`,
		`"retention":{"tombstone_s":604800,"abuse_report_s":604800,"daily_counter_s":86400}`,
	} {
		if !strings.Contains(report.body, want) {
			t.Fatalf("transparency is missing %s: %s", want, report.body)
		}
	}
	plain, err := newWebServer(t, builtClient(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report = get(t, plain.URL+TransparencyPath, nil); strings.Contains(report.body, "operator") {
		t.Fatalf("an unset operator must not appear: %s", report.body)
	}
}
