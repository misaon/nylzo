package server_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
)

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

func TestSecurityHeadersAndNonce(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	required := map[string]string{
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	}
	for _, path := range []string{"/", "/cs/", "/sent", "/report", "/m/" + core.NewID().String(), "/robots.txt", "/api/v1/instance", "/api/v1/messages/" + core.NewID().String(), "/healthz", "/nope"} {
		resp, _ := h.do(request{method: "GET", path: path})
		for k, v := range required {
			if resp.Header.Get(k) != v {
				t.Fatalf("%s: header %s = %q", path, k, resp.Header.Get(k))
			}
		}
		home := path == "/" || path == "/cs/"
		if robots := resp.Header.Get(
			"X-Robots-Tag",
		); home != (robots == "") ||
			(!home && robots != "noindex, nofollow") {
			t.Fatalf("%s: X-Robots-Tag = %q, only the home pages may be indexed (D80, D82)", path, robots)
		}
		if csp := resp.Header.Get(
			"Content-Security-Policy",
		); !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") &&
			csp != "default-src 'none'; frame-ancestors 'none'" {
			t.Fatalf("%s: a response other than HTML carries CSP %q", path, csp)
		}
	}
	specCSP := htmlCSPFromSpec(t)
	nonces := map[string]bool{}
	pages := []string{"/", "/cs/", "/sent", "/report", "/m/" + core.NewID().String()}
	for i := range 2 * len(pages) {
		resp, body := h.do(request{method: "GET", path: pages[i%len(pages)]})
		csp := resp.Header.Get("Content-Security-Policy")
		start := strings.Index(csp, "'nonce-") + len("'nonce-")
		nonce := csp[start : start+strings.Index(csp[start:], "'")]
		if len(nonce) < 22 || csp != strings.ReplaceAll(specCSP, "{nonce}", nonce) {
			t.Fatalf("csp %q differs from spec section 9 %q", csp, specCSP)
		}
		if nonces[nonce] {
			t.Fatal("nonce repeated")
		}
		nonces[nonce] = true
		if !strings.Contains(string(body), `nonce="`+nonce+`"`) ||
			strings.Contains(string(body), "__NYLZO_CSP_NONCE__") {
			t.Fatal("nonce not injected into the page")
		}
	}
	resp, body := h.do(
		request{
			method:  "GET",
			path:    "/m/" + core.NewID().String(),
			headers: map[string]string{"User-Agent": "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"},
		},
	)
	if resp.StatusCode != 200 || strings.Contains(string(body), "<script") ||
		strings.Contains(string(body), "<style") ||
		!strings.Contains(string(body), `name="viewport"`) {
		t.Fatalf("preview agent page: %d %s", resp.StatusCode, body)
	}
	nonceAttr := regexp.MustCompile(`nonce="[^"]*"`)
	_, home := h.do(request{method: "GET", path: "/"})
	for _, path := range []string{"/", "/cs/", "/sent"} {
		_, page := h.do(request{method: "GET", path: path})
		if client, err := server.EmbeddedWebClient(); err != nil ||
			client.Built && strings.Contains(string(page), `name="robots"`) {
			t.Fatalf("%s: the built client HTML must leave robots to the X-Robots-Tag header (D80): %v", path, err)
		}
	}
	if !strings.Contains(string(body), `name="robots" content="noindex, nofollow"`) {
		t.Fatal("the preview page must stay noindex")
	}
	if _, botHome := h.do(
		request{
			method:  "GET",
			path:    "/",
			headers: map[string]string{"User-Agent": "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"},
		},
	); nonceAttr.ReplaceAllString(
		string(botHome),
		"",
	) != nonceAttr.ReplaceAllString(
		string(home),
		"",
	) {
		t.Fatalf("a preview agent must get the normal home page: %s", botHome)
	}
	for _, path := range []string{"/m/" + core.NewID().String(), "/api/v1/instance"} {
		resp, _ := h.do(request{method: "GET", path: path, headers: map[string]string{"Sec-Purpose": "prefetch"}})
		if resp.StatusCode != 503 {
			t.Fatalf("Sec-Purpose on %s: %d", path, resp.StatusCode)
		}
	}
	if resp, _ := h.do(request{method: "GET", path: "/m/not-an-id"}); resp.StatusCode != 404 {
		t.Fatal("malformed page id")
	}
	resp, body = h.do(
		request{
			method:  "GET",
			path:    "/api/v1/messages/not-an-id",
			headers: map[string]string{"Authorization": core.AuthScheme + " short"},
		},
	)
	if resp.StatusCode != 404 || string(body) != `{"error":"not_found"}` {
		t.Fatalf("malformed API id: %d %s", resp.StatusCode, body)
	}
}

func TestCreateValidation(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFScrypt, "pw", 1, 3600, "v")
	cases := []struct {
		name    string
		mutate  func(t *testing.T, r *http.Request)
		status  int
		errCode string
	}{
		{"no-header", func(t *testing.T, r *http.Request) { r.Header.Del("X-Nylzo-Create") }, 400, "bad_request"},
		{
			"bad-base64",
			func(t *testing.T, r *http.Request) { r.Header.Set("X-Nylzo-Create", "***") },
			400,
			"bad_request",
		},
		{"unknown-field", func(t *testing.T, r *http.Request) {
			r.Header.Set("X-Nylzo-Create", h.withIDToken(mutateHeader(t, m, func(f map[string]any) { f["extra"] = 1 })))
		}, 400, "bad_request"},
		{"size-mismatch", func(t *testing.T, r *http.Request) {
			r.Header.Set("X-Nylzo-Create", h.withIDToken(m.createHeader(int64(len(m.object)+1))))
		}, 400, "bad_request"},
		{"size-above-limit-mismatch", func(t *testing.T, r *http.Request) {
			r.Header.Set("X-Nylzo-Create", h.withIDToken(m.createHeader(config.Defaults().Limits.MaxObjectBytes+1)))
		}, 400, "bad_request"},
		{"chunked", func(t *testing.T, r *http.Request) { r.ContentLength = -1 }, 411, "length_required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", h.ts.URL+"/api/v1/messages", strings.NewReader(string(m.object)))
			r.Header.Set("X-Nylzo-Create", h.withIDToken(m.createHeader(0)))
			c.mutate(t, r)
			resp, err := h.client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != c.status || decodeJSON(t, body)["error"] != c.errCode {
				t.Fatalf("%d %s", resp.StatusCode, body)
			}
		})
	}
	wrongKDF := newMessage(t, core.KDFNone, "", 1, 3600, "v")
	header := wrongKDF.createHeader(0)
	wrongKDF.object[1] = byte(core.KDFScrypt)
	resp, _ := h.do(
		request{
			method:  "POST",
			path:    "/api/v1/messages",
			body:    wrongKDF.object,
			headers: map[string]string{"X-Nylzo-Create": header},
		},
	)
	if resp.StatusCode != 400 {
		t.Fatalf("kdf mismatch between header and envelope: %d", resp.StatusCode)
	}
	if h.blobExists(wrongKDF.id) {
		t.Fatal("rejected object stored")
	}
	tooMany := newMessage(t, core.KDFNone, "", 11, 3600, "v")
	if resp, _ := h.create(tooMany); resp.StatusCode != 400 {
		t.Fatal("opens_max above the instance limit accepted")
	}
	tooLong := newMessage(t, core.KDFNone, "", 1, 8*24*3600, "v")
	if resp, _ := h.create(tooLong); resp.StatusCode != 400 {
		t.Fatal("ttl above the instance limit accepted")
	}
	h.mustCreate(m)
	if resp, _ := h.create(m); resp.StatusCode != 409 {
		t.Fatal("duplicate id")
	}
}

func TestAK22SlowConnectionsAndLyingContentLength(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	addr := h.listen()
	var conns []net.Conn
	t.Cleanup(func() {
		for _, c := range conns {
			c.Close()
		}
	})
	for range 100 {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(c, "GET /healthz HTTP/1.1\r\nHost: nylzo\r\nX-Slow: ")
		conns = append(conns, c)
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "liar")
	for i := range 100 {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		liar := newMessage(t, core.KDFNone, "", 1, 3600, "liar")
		fmt.Fprintf(
			c,
			"POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nX-Forwarded-For: 10.9.%d.1\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n",
			i,
			len(liar.object),
			h.withIDToken(liar.createHeader(0)),
		)
		c.Write(liar.object[:20])
		conns = append(conns, c)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("healthz during slowloris: %v", err)
	}
	resp.Body.Close()
	// gstack-timing-ok: AK22 requires other requests to be answered within 2 s during a slowloris
	if resp.StatusCode != 200 || time.Since(start) > 2*time.Second {
		t.Fatalf("healthz %d after %s", resp.StatusCode, time.Since(start))
	}
	r, _ := http.NewRequest("POST", "http://"+addr+"/api/v1/messages", strings.NewReader(string(m.object)))
	r.Header.Set("X-Nylzo-Create", h.withIDToken(m.createHeader(0)))
	r.Header.Set("X-Forwarded-For", "192.0.2.1")
	resp, err = client.Do(r)
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("create during slowloris: %v %v", err, resp)
	}
	resp.Body.Close()
	tooLarge := h.cfg.Limits.MaxObjectBytes + 1
	checks := []struct {
		name         string
		header       string
		sizeOverride int64
		status       string
	}{
		{"no-content-length", "", 0, "411"},
		{"size-mismatch", "Content-Length: 5\r\n", 0, "400"},
		{"too-large", fmt.Sprintf("Content-Length: %d\r\n", tooLarge), tooLarge, "413"},
		{"too-large-mismatch", fmt.Sprintf("Content-Length: %d\r\n", tooLarge), 0, "400"},
	}
	for _, c := range checks {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		probe := newMessage(t, core.KDFNone, "", 1, 3600, "probe")
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprintf(
			conn,
			"POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nX-Forwarded-For: 192.0.2.9\r\n%sX-Nylzo-Create: %s\r\n\r\n",
			c.header,
			h.withIDToken(probe.createHeader(c.sizeOverride)),
		)
		line, err := bufio.NewReader(conn).ReadString('\n')
		conn.Close()
		if err != nil || !strings.Contains(line, " "+c.status+" ") {
			t.Fatalf("%s: %q %v (response must arrive before any body is sent)", c.name, line, err)
		}
	}
}

func TestH2CReceiveBufferIsBounded(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	c := h.srv.HTTPServer().HTTP2
	if c == nil || c.MaxReceiveBufferPerStream < 64<<10 || c.MaxReceiveBufferPerStream > 256<<10 ||
		c.MaxReceiveBufferPerConnection < 4*c.MaxReceiveBufferPerStream ||
		c.MaxReceiveBufferPerConnection > 1<<20 {
		t.Fatalf("h2c must bound unread body per stream and leave the connection room for other streams: %+v", c)
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.BucketPerSecond = 1
	cfg.Rates.BucketBurst = 2
	h := newHarness(t, cfg)
	want := map[string]string{
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
		"Permissions-Policy":           "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
	}
	check := func(name string, resp *http.Response, status int) {
		t.Helper()
		if resp.StatusCode != status {
			t.Fatalf("%s: status %d, want %d", name, resp.StatusCode, status)
		}
		for k, v := range want {
			if resp.Header.Get(k) != v {
				t.Fatalf("%s: %s = %q", name, k, resp.Header.Get(k))
			}
		}
		robots := "noindex, nofollow"
		if name == "page" || name == "page-head" {
			robots = ""
		}
		if resp.Header.Get("X-Robots-Tag") != robots {
			t.Fatalf("%s: X-Robots-Tag = %q", name, resp.Header.Get("X-Robots-Tag"))
		}
	}
	resp, _ := h.do(request{method: "GET", path: "/"})
	check("page", resp, 200)
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "'nonce-") {
		t.Fatal("CSP missing on /")
	}
	resp, _ = h.do(request{method: "HEAD", path: "/"})
	check("page-head", resp, 200)
	resp, _ = h.do(request{method: "POST", path: "/"})
	check("page-post", resp, 405)
	resp, _ = h.do(
		request{method: "GET", path: "/api/v1/instance", headers: map[string]string{"Sec-Purpose": "prefetch"}},
	)
	check("sec-purpose", resp, 503)
	resp, _ = h.do(request{method: "POST", path: "/api/v1/messages", body: []byte("x")})
	check("create-400", resp, 400)
	h.do(request{method: "GET", path: "/api/v1/instance"})
	resp, _ = h.do(request{method: "GET", path: "/api/v1/instance"})
	check("throttled", resp, 429)
}

func TestRejectedCreateKeepsH2CConnection(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	addr := h.listen()
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &protocols}, Timeout: 5 * time.Second}
	var reused []bool
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = append(reused, info.Reused) }}
	send := func(method, path string) *http.Response {
		r, _ := http.NewRequestWithContext(
			httptrace.WithClientTrace(t.Context(), trace),
			method,
			"http://"+addr+path,
			nil,
		)
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}
	if resp := send("POST", "/api/v1/messages"); resp.StatusCode != 400 || resp.ProtoMajor != 2 {
		t.Fatalf("rejected create over h2c: %d HTTP/%d", resp.StatusCode, resp.ProtoMajor)
	}
	if resp := send("GET", "/healthz"); resp.StatusCode != 200 || resp.ProtoMajor != 2 {
		t.Fatalf("follow-up over h2c: %d HTTP/%d", resp.StatusCode, resp.ProtoMajor)
	}
	if len(reused) != 2 || !reused[1] {
		t.Fatalf("a rejected create closed the shared h2c connection: %v", reused)
	}
}

func TestInstanceOffersNoPassphraseProfile(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	_, body := h.do(request{method: "GET", path: "/api/v1/instance"})
	if _, ok := decodeJSON(t, body)["limits"].(map[string]any)["default_kdf_id"]; ok {
		t.Fatal("the instance still reports default_kdf_id, but D71 leaves one profile")
	}
}

func TestDefaultObjectLimitMatchesSpec(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	_, body := h.do(request{method: "GET", path: "/api/v1/instance"})
	limits := decodeJSON(t, body)["limits"].(map[string]any)
	if limits["max_object_bytes"] != float64(5373983) || limits["max_text_bytes"] != float64(core.MaxTextBytes) {
		t.Fatalf("instance reports %v", limits)
	}
}
