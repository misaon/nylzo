package localclient_test

import (
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/misaon/nylzo/internal/localclient"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	localHost = "127.0.0.1:54321"
	local     = "http://" + localHost
	pageIndex = `<!doctype html><html><head><script type="importmap">{"integrity":{}}</script>` +
		`<!--nylzo:public--><meta name="nylzo-instance" content="__NYLZO_PUBLIC_URL__"><!--/nylzo:public-->` +
		`<script type="module" src="/assets/index-a1.js"></script></head><body></body></html>`
	appJS = "console.log('nylzo')"
)

type instance struct {
	mutex    sync.Mutex
	requests []*http.Request
	server   *httptest.Server
}

func (i *instance) seen() []*http.Request {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	return append([]*http.Request(nil), i.requests...)
}

func newInstance(t *testing.T, answer http.HandlerFunc) *instance {
	t.Helper()
	recorded := &instance{}
	recorded.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.mutex.Lock()
		recorded.requests = append(recorded.requests, r.Clone(t.Context()))
		recorded.mutex.Unlock()
		answer(w, r)
	}))
	t.Cleanup(recorded.server.Close)
	return recorded
}

func answerJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write([]byte(`{"ok":true}`))
}

func webBuild(t *testing.T) webclient.Build {
	t.Helper()
	build, err := webclient.Load(fstest.MapFS{
		"placeholder.html":        {Data: []byte("placeholder")},
		"dist/index.html":         {Data: []byte(pageIndex)},
		"dist/home.en.html":       {Data: []byte(strings.Replace(pageIndex, "<body>", "<body><h1>en</h1>", 1))},
		"dist/home.cs.html":       {Data: []byte(strings.Replace(pageIndex, "<body>", "<body><h1>cs</h1>", 1))},
		"dist/assets/index-a1.js": {Data: []byte(appJS)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return build
}

func newLocal(t *testing.T, upstream *instance, notify func(localclient.Notice)) *localclient.Server {
	t.Helper()
	address := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 54321}
	server, err := localclient.New(address, &localclient.Config{
		Instance: upstream.server.URL,
		Build:    webBuild(t),
		Version:  "test",
		Notify:   notify,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

type call struct {
	method  string
	path    string
	headers map[string]string
	body    string
}

func fetchFromPage(method, path string) call {
	headers := map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors", "Sec-Fetch-Dest": "empty"}
	if method != http.MethodGet {
		headers["Origin"] = local
	}
	return call{method: method, path: path, headers: headers}
}

func (c call) with(name, value string) call {
	headers := map[string]string{}
	maps.Copy(headers, c.headers)
	if value == "" {
		delete(headers, name)
	} else {
		headers[name] = value
	}
	c.headers = headers
	return c
}

func do(t *testing.T, server *localclient.Server, request call) *httptest.ResponseRecorder {
	t.Helper()
	outgoing := httptest.NewRequestWithContext(
		t.Context(),
		request.method,
		local+request.path,
		strings.NewReader(request.body),
	)
	for name, value := range request.headers {
		if name == "Host" {
			outgoing.Host = value
			continue
		}
		outgoing.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, outgoing)
	return recorder
}

func TestListenOnlyOnALoopbackAddress(t *testing.T) {
	t.Parallel()
	listener, err := localclient.Listen(t.Context(), localclient.DefaultAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if address, ok := listener.Addr().(*net.TCPAddr); !ok || !address.IP.IsLoopback() || address.Port == 0 {
		t.Fatalf("the default address must be a random loopback port, got %v", listener.Addr())
	}
	for _, address := range []string{
		"0.0.0.0:0", ":0", "[::]:0", "localhost:0", "192.0.2.1:0", "[::ffff:127.0.0.1]:0", "[fe80::1%lo0]:0", "nylzo",
	} {
		if refused, listenErr := localclient.Listen(
			t.Context(),
			address,
		); !errors.Is(
			listenErr,
			localclient.ErrNotLoopback,
		) {
			if refused != nil {
				refused.Close()
			}
			t.Errorf("%s: %v", address, listenErr)
		}
	}
	upstream := newInstance(t, answerJSON)
	for _, address := range []net.Addr{
		&net.TCPAddr{IP: net.IPv4zero, Port: 54321},
		&net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 54321},
		&net.UnixAddr{Name: "/tmp/nylzo.sock", Net: "unix"},
	} {
		_, newErr := localclient.New(address, &localclient.Config{Instance: upstream.server.URL, Build: webBuild(t)})
		if !errors.Is(newErr, localclient.ErrNotLoopback) {
			t.Errorf("%v: %v", address, newErr)
		}
	}
}

func TestOnlyTheListenAddressIsServed(t *testing.T) {
	t.Parallel()
	upstream := newInstance(t, answerJSON)
	server := newLocal(t, upstream, nil)
	for _, host := range []string{
		"rebind.attacker.example", "rebind.attacker.example:54321", "localhost:54321",
		"127.0.0.1:54322", "127.0.0.2:54321", "[::1]:54321", "127.0.0.1",
	} {
		for _, request := range []call{{method: http.MethodGet, path: "/"}, fetchFromPage(http.MethodGet, "/api/v1/instance")} {
			response := do(t, server, request.with("Host", host))
			if response.Code != http.StatusMisdirectedRequest || strings.Contains(response.Body.String(), "html") {
				t.Fatalf("Host %s, %s: %d", host, request.path, response.Code)
			}
		}
	}
	if seen := upstream.seen(); len(seen) != 0 {
		t.Fatalf("a request for another host reached the instance: %v", seen[0].URL)
	}
	if response := do(t, server, call{method: http.MethodGet, path: "/"}); response.Code != http.StatusOK {
		t.Fatalf("its own host: %d", response.Code)
	}
}

func TestRelayAdmitsOnlyFetchCallsFromItsOwnPages(t *testing.T) {
	t.Parallel()
	upstream := newInstance(t, answerJSON)
	server := newLocal(t, upstream, nil)
	post := fetchFromPage(http.MethodPost, "/api/v1/messages")
	get := fetchFromPage(http.MethodGet, "/api/v1/instance")
	for name, request := range map[string]call{
		"another site":            post.with("Sec-Fetch-Site", "cross-site"),
		"another port":            post.with("Sec-Fetch-Site", "same-site").with("Origin", "http://127.0.0.1:3000"),
		"same site, same origin":  post.with("Origin", "http://127.0.0.1:3000"),
		"no fetch metadata":       post.with("Sec-Fetch-Site", ""),
		"typed in the address":    get.with("Sec-Fetch-Site", "none").with("Sec-Fetch-Mode", "navigate"),
		"link click":              get.with("Sec-Fetch-Mode", "navigate").with("Sec-Fetch-Dest", "document"),
		"script element":          get.with("Sec-Fetch-Mode", "no-cors").with("Sec-Fetch-Dest", "script"),
		"worker":                  get.with("Sec-Fetch-Dest", "worker"),
		"mutation without origin": post.with("Origin", ""),
		"opaque origin":           post.with("Origin", "null"),
		"origin of the instance":  post.with("Origin", upstream.server.URL),
		"localhost origin":        post.with("Origin", "http://localhost:54321"),
		"transparency elsewhere":  {method: http.MethodGet, path: "/.well-known/nylzo/transparency.json"},
	} {
		if response := do(t, server, request); response.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, response.Code)
		}
	}
	duplicated := httptest.NewRequestWithContext(t.Context(), http.MethodPost, local+"/api/v1/messages", http.NoBody)
	duplicated.Header["Sec-Fetch-Site"] = []string{"same-origin", "cross-site"}
	duplicated.Header.Set("Origin", local)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, duplicated)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("duplicated metadata: %d", recorder.Code)
	}
	if seen := upstream.seen(); len(seen) != 0 {
		t.Fatalf("a refused request reached the instance: %s %s", seen[0].Method, seen[0].URL.Path)
	}
	for _, request := range []call{post, get, fetchFromPage(http.MethodGet, "/.well-known/nylzo/transparency.json")} {
		if response := do(t, server, request); response.Code != http.StatusOK {
			t.Errorf("%s %s from its own page: %d %s", request.method, request.path, response.Code, response.Body)
		}
	}
}

func TestRelayCarriesOnlyTheAPIAsANativeClient(t *testing.T) {
	t.Parallel()
	upstream := newInstance(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/messages/AAAAAAAAAAAAAAAAAAAAAA/blob":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "script-src *")
			w.Header().Set("Set-Cookie", "tracked=1")
			w.Header().Set("X-Nylzo-Policy", "kept")
			w.Write([]byte("<script>alert(1)</script>"))
		case "/api/v1/challenge":
			http.Redirect(w, r, "https://attacker.example/", http.StatusFound)
		default:
			answerJSON(w, r)
		}
	})
	server := newLocal(t, upstream, nil)
	request := fetchFromPage(http.MethodGet, "/api/v1/messages/AAAAAAAAAAAAAAAAAAAAAA/blob").
		with("Cookie", "session=1").with("Referer", local+"/m/x").with("Accept-Language", "cs").
		with("Authorization", "Nylzo-Sig signed").with("X-Nylzo-Lease", "lease")
	response := do(t, server, request)
	header := response.Header()
	if response.Code != http.StatusOK || header.Get("Content-Type") != "application/octet-stream" ||
		header.Get("Content-Security-Policy") != webclient.NonHTMLCSP || header.Get("Set-Cookie") != "" ||
		header.Get("X-Nylzo-Policy") != "kept" || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("a relayed answer must not be able to run in the local origin: %d %v", response.Code, header)
	}
	seen := upstream.seen()
	if len(seen) != 1 {
		t.Fatalf("%d requests reached the instance", len(seen))
	}
	for name := range seen[0].Header {
		if !map[string]bool{"Authorization": true, "X-Nylzo-Lease": true, "User-Agent": true, "Accept-Encoding": true}[name] {
			t.Errorf("the relay passed the browser header %s to the instance", name)
		}
	}
	if seen[0].UserAgent() != "nylzo-cli" || seen[0].Header.Get("Authorization") != "Nylzo-Sig signed" {
		t.Fatalf("the relay must look like the command line client: %v", seen[0].Header)
	}
	redirect := do(t, server, fetchFromPage(http.MethodGet, "/api/v1/challenge"))
	if redirect.Code != http.StatusBadGateway || redirect.Header().Get("Location") != "" {
		t.Fatalf("a redirect from the instance must not reach the browser: %d %v", redirect.Code, redirect.Header())
	}
	before := len(upstream.seen())
	for _, path := range []string{
		"/", "/cs/", "/sent", "/report", "/transparency", "/m/AAAAAAAAAAAAAAAAAAAAAA", "/open", "/assets/index-a1.js",
		webclient.ManifestPath, "/robots.txt", "/api/v2/messages", "/api/v1/%2e%2e/assets/index-a1.js",
		"/api/v1/instance?callback=x", "/assets/../api/v1/instance", "/api/v1/messages%2Finstance",
	} {
		do(t, server, fetchFromPage(http.MethodGet, path))
		do(t, server, call{method: http.MethodGet, path: path})
	}
	if after := upstream.seen(); len(after) != before {
		t.Fatalf("a page, asset or non-API path reached the instance: %s", after[before].URL)
	}
}

func TestPagesAreTheEmbeddedClientWithTheInstanceAddress(t *testing.T) {
	t.Parallel()
	upstream := newInstance(t, answerJSON)
	server := newLocal(t, upstream, nil)
	build := webBuild(t)
	pages, err := build.Render(upstream.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for path, template := range map[string]string{
		"/": "/", "/cs/": "/cs/", "/sent": "/sent", "/m/AAAAAAAAAAAAAAAAAAAAAA": "/m/{id}", "/open": "/sent",
	} {
		response := do(t, server, call{method: http.MethodGet, path: path})
		want := pages.For(template)
		if response.Code != http.StatusOK || response.Body.String() != string(want.Body()) ||
			response.Header().Get("Content-Security-Policy") != want.Policy() ||
			response.Header().Get("Cross-Origin-Embedder-Policy") != "require-corp" ||
			!strings.Contains(response.Body.String(), `content="`+upstream.server.URL+`"`) {
			t.Fatalf("%s: %d %v %s", path, response.Code, response.Header(), response.Body)
		}
	}
	if response := do(
		t,
		server,
		call{method: http.MethodGet, path: "/m/not-an-id"},
	); response.Code != http.StatusNotFound {
		t.Fatalf("an invalid id: %d", response.Code)
	}
	if asset := do(t, server, call{method: http.MethodGet, path: "/assets/index-a1.js"}); asset.Body.String() != appJS {
		t.Fatalf("asset: %d %s", asset.Code, asset.Body)
	}
}

func TestHandoffIsSingleUse(t *testing.T) {
	t.Parallel()
	upstream := newInstance(t, answerJSON)
	var mutex sync.Mutex
	var notices []localclient.Notice
	server := newLocal(t, upstream, func(notice localclient.Notice) {
		mutex.Lock()
		notices = append(notices, notice)
		mutex.Unlock()
	})
	redeem := func(token string) call {
		request := fetchFromPage(http.MethodPost, localclient.HandoffPath)
		request.body = token
		return request
	}
	token, err := server.Offer("/m/AAAAAAAAAAAAAAAAAAAAAA", "1.0.secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 26 {
		t.Fatalf("the token must carry at least 128 random bits: %q", token)
	}
	for name, request := range map[string]call{
		"another site": redeem(token).with("Sec-Fetch-Site", "cross-site").with("Origin", "https://attacker.example"),
		"a navigation": redeem(token).with("Sec-Fetch-Mode", "navigate"),
		"another host": redeem(token).with("Host", "rebind.attacker.example:54321"),
	} {
		if response := do(t, server, request); response.Code == http.StatusOK {
			t.Fatalf("%s took the link", name)
		}
	}
	if response := do(t, server, redeem(token+"x")); response.Code != http.StatusNotFound {
		t.Fatalf("a wrong token: %d", response.Code)
	}
	first := do(t, server, redeem(token))
	if first.Code != http.StatusOK ||
		first.Body.String() != `{"path":"/m/AAAAAAAAAAAAAAAAAAAAAA","fragment":"1.0.secret"}` ||
		first.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("first redemption: %d %s", first.Code, first.Body)
	}
	if second := do(
		t,
		server,
		redeem(token),
	); second.Code != http.StatusNotFound ||
		strings.Contains(second.Body.String(), "secret") {
		t.Fatalf("second redemption: %d %s", second.Code, second.Body)
	}
	mutex.Lock()
	got := append([]localclient.Notice(nil), notices...)
	mutex.Unlock()
	want := []localclient.Notice{localclient.HandoffRefused, localclient.HandoffTaken, localclient.HandoffRefused}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("notices %v, want %v", got, want)
	}

	expired, err := server.Offer("/m/AAAAAAAAAAAAAAAAAAAAAA", "1.0.secret", -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if response := do(t, server, redeem(expired)); response.Code != http.StatusNotFound {
		t.Fatalf("an expired handoff: %d", response.Code)
	}
	withdrawn, err := server.Offer("/m/AAAAAAAAAAAAAAAAAAAAAA", "1.0.secret", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server.Withdraw()
	if response := do(t, server, redeem(withdrawn)); response.Code != http.StatusNotFound {
		t.Fatalf("a withdrawn handoff: %d", response.Code)
	}
	if seen := upstream.seen(); len(seen) != 0 {
		t.Fatalf("the handoff reached the instance: %v", seen[0].URL)
	}
	if body, _ := io.ReadAll(
		do(t, server, call{method: http.MethodGet, path: "/open"}).Body,
	); strings.Contains(
		string(body),
		"secret",
	) {
		t.Fatal("the handoff page must not carry the link")
	}
}
