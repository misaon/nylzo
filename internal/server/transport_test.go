package server_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
)

func TestSecurityHeadersAndNonce(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	required := map[string]string{
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"X-Robots-Tag":                 "noindex, nofollow",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	}
	for _, path := range []string{"/", "/m/" + core.NewID().String(), "/api/v1/instance", "/api/v1/messages/" + core.NewID().String(), "/healthz", "/nope"} {
		resp, _ := h.do(request{method: "GET", path: path})
		for k, v := range required {
			if resp.Header.Get(k) != v {
				t.Fatalf("%s: header %s = %q", path, k, resp.Header.Get(k))
			}
		}
	}
	nonces := map[string]bool{}
	for i := 0; i < 5; i++ {
		resp, body := h.do(request{method: "GET", path: "/m/" + core.NewID().String()})
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "font-src 'self'") || strings.Contains(csp, "unsafe") || strings.Contains(csp, "data:") || !strings.Contains(csp, "require-trusted-types-for 'script'") {
			t.Fatalf("csp: %s", csp)
		}
		start := strings.Index(csp, "'nonce-") + len("'nonce-")
		nonce := csp[start : start+strings.Index(csp[start:], "'")]
		if nonces[nonce] {
			t.Fatal("nonce repeated")
		}
		nonces[nonce] = true
		if !strings.Contains(string(body), `nonce="`+nonce+`"`) || strings.Contains(string(body), "__NYLZO_CSP_NONCE__") {
			t.Fatal("nonce not injected into the page")
		}
	}
	resp, body := h.do(request{method: "GET", path: "/m/" + core.NewID().String(), headers: map[string]string{"User-Agent": "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"}})
	if resp.StatusCode != 200 || strings.Contains(string(body), "<script") || strings.Contains(string(body), "<style") || !strings.Contains(string(body), `name="viewport"`) {
		t.Fatalf("preview agent page: %d %s", resp.StatusCode, body)
	}
	nonceAttr := regexp.MustCompile(`nonce="[^"]*"`)
	_, home := h.do(request{method: "GET", path: "/"})
	if _, botHome := h.do(request{method: "GET", path: "/", headers: map[string]string{"User-Agent": "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"}}); nonceAttr.ReplaceAllString(string(botHome), "") != nonceAttr.ReplaceAllString(string(home), "") {
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
	resp, body = h.do(request{method: "GET", path: "/api/v1/messages/not-an-id", headers: map[string]string{"Authorization": "Bearer short"}})
	if resp.StatusCode != 404 || string(body) != `{"error":"not_found"}` {
		t.Fatalf("malformed API id: %d %s", resp.StatusCode, body)
	}
}

func TestCreateValidation(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFScrypt, "pw", 1, 3600, "v")
	cases := []struct {
		name    string
		mutate  func(r *http.Request)
		status  int
		errCode string
	}{
		{"no-header", func(r *http.Request) { r.Header.Del("X-Nylzo-Create") }, 400, "bad_request"},
		{"bad-base64", func(r *http.Request) { r.Header.Set("X-Nylzo-Create", "***") }, 400, "bad_request"},
		{"unknown-field", func(r *http.Request) {
			r.Header.Set("X-Nylzo-Create", mutateHeader(t, m, func(f map[string]any) { f["extra"] = 1 }))
		}, 400, "bad_request"},
		{"size-mismatch", func(r *http.Request) { r.Header.Set("X-Nylzo-Create", m.createHeader(int64(len(m.object)+1))) }, 400, "bad_request"},
		{"size-above-limit-mismatch", func(r *http.Request) {
			r.Header.Set("X-Nylzo-Create", m.createHeader(server.Defaults().Limits.MaxObjectBytes+1))
		}, 400, "bad_request"},
		{"chunked", func(r *http.Request) { r.ContentLength = -1 }, 411, "length_required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", h.ts.URL+"/api/v1/messages", strings.NewReader(string(m.object)))
			r.Header.Set("X-Nylzo-Create", m.createHeader(0))
			c.mutate(r)
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
	wrongKDF.object[1] = 2
	resp, _ := h.do(request{method: "POST", path: "/api/v1/messages", body: wrongKDF.object, headers: map[string]string{"X-Nylzo-Create": header}})
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
	for i := 0; i < 100; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(c, "GET /healthz HTTP/1.1\r\nHost: nylzo\r\nX-Slow: ")
		conns = append(conns, c)
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "liar")
	for i := 0; i < 100; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		liar := newMessage(t, core.KDFNone, "", 1, 3600, "liar")
		fmt.Fprintf(c, "POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nX-Forwarded-For: 10.9.%d.1\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n", i, len(liar.object), liar.createHeader(0))
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
	r.Header.Set("X-Nylzo-Create", m.createHeader(0))
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
		fmt.Fprintf(conn, "POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nX-Forwarded-For: 192.0.2.9\r\n%sX-Nylzo-Create: %s\r\n\r\n", c.header, probe.createHeader(c.sizeOverride))
		line, err := bufio.NewReader(conn).ReadString('\n')
		conn.Close()
		if err != nil || !strings.Contains(line, " "+c.status+" ") {
			t.Fatalf("%s: %q %v (response must arrive before any body is sent)", c.name, line, err)
		}
	}
}

func TestClientAddr(t *testing.T) {
	cfg := server.Defaults()
	cfg.Pepper = pepperA
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}
	cases := []struct {
		remote, xff, realIP, mode, want string
	}{
		{"203.0.113.1:1234", "", "", "", "203.0.113.1"},
		{"203.0.113.1:1234", "198.51.100.1", "", "", "203.0.113.1"},
		{"10.0.0.5:1234", "198.51.100.1", "", "", "198.51.100.1"},
		{"10.0.0.5:1234", "198.51.100.1, 10.0.0.9", "", "", "198.51.100.1"},
		{"10.0.0.5:1234", "192.0.2.4, 198.51.100.1, 10.0.0.9", "", "", "198.51.100.1"},
		{"10.0.0.5:1234", "garbage", "", "", "10.0.0.5"},
		{"10.0.0.5:1234", "", "", "", "10.0.0.5"},
		{"[::ffff:203.0.113.7]:1234", "", "", "", "203.0.113.7"},
		{"203.0.113.1:1234", "", "198.51.100.2", "", "203.0.113.1"},
		{"10.0.0.5:1234", "", "198.51.100.2", "", "10.0.0.5"},
		{"[fd00::5]:1234", "2001:db8::7, fd00::9", "", "", "2001:db8::7"},
		{"[2001:db8::1]:1234", "2001:db8::7", "", "", "2001:db8::1"},
		{"203.0.113.1:1234", "198.51.100.1", "198.51.100.2", "scaleway", "198.51.100.2"},
		{"203.0.113.1:1234", "", "", "scaleway", "203.0.113.1"},
	}
	for _, c := range cases {
		got := server.ClientAddrForTest(cfg, c.mode, c.remote, c.xff, c.realIP)
		if got.String() != c.want {
			t.Fatalf("remote %s xff %q real %q mode %q: got %s want %s", c.remote, c.xff, c.realIP, c.mode, got, c.want)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{
		"NYLZO_PEPPER":               strings.Repeat("ab", 32),
		"NYLZO_PEPPER_PREVIOUS":      strings.Repeat("cd", 32),
		"NYLZO_TRUSTED_PROXIES":      "10.0.0.0/8, 192.0.2.1",
		"NYLZO_PROXY_MODE":           "scaleway",
		"NYLZO_SWEEP_INTERVAL":       "15m",
		"NYLZO_MAX_TTL":              "172800",
		"NYLZO_MAX_OPENS":            "3",
		"NYLZO_RATE_CREATE_HOUR":     "7",
		"NYLZO_GLOBAL_DAILY_CREATES": "500",
		"PORT":                       "9090",
	}
	cfg, err := server.FromEnv(func(k string) string { return env[k] }, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9090" || len(cfg.TrustedProxies) != 2 || cfg.ProxyMode != "scaleway" || cfg.SweepInterval != 15*time.Minute || cfg.Limits.MaxTTL != 48*time.Hour || cfg.Limits.MaxOpens != 3 || cfg.Rates.CreatePerHour != 7 || cfg.Rates.GlobalDailyCreates != 500 || len(cfg.PepperPrevious) != 32 {
		t.Fatalf("config %+v", cfg)
	}
	if _, err := server.FromEnv(func(string) string { return "" }, true); err == nil {
		t.Fatal("serve without pepper accepted")
	}
	if _, err := server.FromEnv(func(string) string { return "" }, false); err != nil {
		t.Fatalf("sweep-once without pepper: %v", err)
	}
	if _, err := server.FromEnv(func(k string) string {
		if k == "NYLZO_PEPPER" {
			return "short"
		}
		return ""
	}, true); err == nil {
		t.Fatal("bad pepper accepted")
	}
	if _, err := server.FromEnv(func(k string) string {
		if k == "NYLZO_PROXY_MODE" {
			return "cloudflare"
		}
		return env[k]
	}, true); err == nil {
		t.Fatal("unknown proxy mode accepted")
	}
	tooBig := strconv.Itoa(core.MaxFileBytes + 1)
	if _, err := server.FromEnv(func(k string) string {
		if k == "NYLZO_MAX_FILE_BYTES" {
			return tooBig
		}
		return env[k]
	}, true); err == nil {
		t.Fatal("NYLZO_MAX_FILE_BYTES above the protocol limit accepted")
	}
	lower, err := server.FromEnv(func(k string) string {
		if k == "NYLZO_MAX_FILE_BYTES" {
			return "1048576"
		}
		return env[k]
	}, true)
	if err != nil || lower.Limits.MaxObjectBytes != core.ObjectLimit(1048576) {
		t.Fatalf("lower file limit: %v %d", err, lower.Limits.MaxObjectBytes)
	}
}

func TestTrustedProxyInMappedForm(t *testing.T) {
	cfg, err := server.FromEnv(func(k string) string {
		if k == "NYLZO_TRUSTED_PROXIES" {
			return "::ffff:172.17.0.1, ::ffff:10.0.0.0/104"
		}
		return ""
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, remote := range []string{"172.17.0.1:1234", "10.9.8.7:1234"} {
		if got := server.ClientAddrForTest(cfg, "", remote, "198.51.100.9", ""); got.String() != "198.51.100.9" {
			t.Fatalf("proxy %s written in mapped form must be trusted: %s", remote, got)
		}
	}
}

func TestH2CReceiveBufferIsBounded(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	c := h.srv.HTTPServer().HTTP2
	if c == nil || c.MaxReceiveBufferPerStream < 64<<10 || c.MaxReceiveBufferPerStream > 256<<10 || c.MaxReceiveBufferPerConnection < 4*c.MaxReceiveBufferPerStream || c.MaxReceiveBufferPerConnection > 1<<20 {
		t.Fatalf("h2c must bound unread body per stream and leave the connection room for other streams: %+v", c)
	}
}

func TestPoWConcurrencyCapMatchesSpec(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "NYLZO_POW_CONCURRENCY" {
				return v
			}
			return ""
		}
	}
	if _, err := server.FromEnv(env("4"), false); err != nil {
		t.Fatalf("the spec allows 4 concurrent PoW checks: %v", err)
	}
	if _, err := server.FromEnv(env("5"), false); err == nil {
		t.Fatal("NYLZO_POW_CONCURRENCY above 4 accepted")
	}
}

func TestConfigErrorNamesTheVariable(t *testing.T) {
	for name, value := range map[string]string{"NYLZO_POW_CONCURRENCY": "8", "NYLZO_MAX_OPENS": "0", "NYLZO_POW_DIFFICULTY": "9", "NYLZO_DEFAULT_TTL": "30s"} {
		_, err := server.FromEnv(func(k string) string {
			if k == name {
				return value
			}
			return ""
		}, false)
		if !errors.Is(err, server.ErrConfig) || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s=%s: the error must name the variable: %v", name, value, err)
		}
	}
}
