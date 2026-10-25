package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/webclient"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
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
	harness := newHarness(t, baseSettings(pepperA, nil))
	required := map[string]string{
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	}
	for _, path := range []string{"/", "/cs/", "/sent", "/report", "/m/" + core.NewID().String(), "/robots.txt", "/api/v1/instance", "/api/v1/messages/" + core.NewID().String(), "/healthz", "/nope"} {
		response, _ := harness.do(&testRequest{method: http.MethodGet, path: path})
		for k, v := range required {
			if response.Header.Get(k) != v {
				t.Fatalf("%s: header %s = %q", path, k, response.Header.Get(k))
			}
		}
		home := path == "/" || path == "/cs/"
		if robots := response.Header.Get(
			"X-Robots-Tag",
		); home != (robots == "") ||
			(!home && robots != "noindex, nofollow") {
			t.Fatalf("%s: X-Robots-Tag = %q, only the home pages may be indexed (D80, D82)", path, robots)
		}
		if csp := response.Header.Get(
			"Content-Security-Policy",
		); !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") &&
			csp != "default-src 'none'; frame-ancestors 'none'" {
			t.Fatalf("%s: a response other than HTML carries CSP %q", path, csp)
		}
	}
	specCSP := htmlCSPFromSpec(t)
	nonces := map[string]bool{}
	pages := []string{"/", "/cs/", "/sent", "/report", "/m/" + core.NewID().String()}
	for i := range 2 * len(pages) {
		response, body := harness.do(&testRequest{method: http.MethodGet, path: pages[i%len(pages)]})
		csp := response.Header.Get("Content-Security-Policy")
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
	response, body := harness.do(
		&testRequest{
			method:  http.MethodGet,
			path:    "/m/" + core.NewID().String(),
			headers: map[string]string{"User-Agent": "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"},
		},
	)
	if response.StatusCode != http.StatusOK || strings.Contains(string(body), "<script") ||
		strings.Contains(string(body), "<style") ||
		!strings.Contains(string(body), `name="viewport"`) {
		t.Fatalf("preview agent page: %d %s", response.StatusCode, body)
	}
	nonceAttr := regexp.MustCompile(`nonce="[^"]*"`)
	_, home := harness.do(&testRequest{method: http.MethodGet, path: "/"})
	for _, path := range []string{"/", "/cs/", "/sent"} {
		_, page := harness.do(&testRequest{method: http.MethodGet, path: path})
		if client, err := EmbeddedWebClient(); err != nil ||
			client.Built && strings.Contains(string(page), `name="robots"`) {
			t.Fatalf("%s: the built client HTML must leave robots to the X-Robots-Tag header (D80): %v", path, err)
		}
	}
	if !strings.Contains(string(body), `name="robots" content="noindex, nofollow"`) {
		t.Fatal("the preview page must stay noindex")
	}
	if _, botHome := harness.do(
		&testRequest{
			method:  http.MethodGet,
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
		got, _ := harness.do(
			&testRequest{method: http.MethodGet, path: path, headers: map[string]string{"Sec-Purpose": "prefetch"}},
		)
		if got.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("Sec-Purpose on %s: %d", path, got.StatusCode)
		}
	}
	if got, _ := harness.do(
		&testRequest{method: http.MethodGet, path: "/m/not-an-id"},
	); got.StatusCode != http.StatusNotFound {
		t.Fatal("malformed page id")
	}
	response, body = harness.do(
		&testRequest{
			method:  http.MethodGet,
			path:    "/api/v1/messages/not-an-id",
			headers: map[string]string{"Authorization": core.AuthScheme + " short"},
		},
	)
	if response.StatusCode != http.StatusNotFound || string(body) != `{"error":"not_found"}` {
		t.Fatalf("malformed API id: %d %s", response.StatusCode, body)
	}
}

func TestAK22SlowConnectionsAndLyingContentLength(t *testing.T) {
	harness := newHarness(t, baseSettings(pepperA, nil))
	addr := harness.listen()
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
	message := newMessage(t, core.KDFNone, "", 1, 3600, "liar")
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
			harness.withIDToken(liar.createHeader(0)),
		)
		c.Write(liar.object[:20])
		conns = append(conns, c)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	response, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("healthz during slowloris: %v", err)
	}
	response.Body.Close()
	// gstack-timing-ok: AK22 requires other requests to be answered within 2 s during a slowloris
	if response.StatusCode != http.StatusOK || time.Since(start) > 2*time.Second {
		t.Fatalf("healthz %d after %s", response.StatusCode, time.Since(start))
	}
	r, _ := http.NewRequest(
		http.MethodPost,
		"http://"+addr+"/api/v1/messages",
		strings.NewReader(string(message.object)),
	)
	r.Header.Set("X-Nylzo-Create", harness.withIDToken(message.createHeader(0)))
	r.Header.Set("X-Forwarded-For", "192.0.2.1")
	response, err = client.Do(r)
	if err != nil || response.StatusCode != http.StatusCreated {
		t.Fatalf("create during slowloris: %v %v", err, response)
	}
	response.Body.Close()
	tooLarge := harness.settings.Limits.MaxObjectBytes + 1
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
		conn, dialErr := net.Dial("tcp", addr)
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		probe := newMessage(t, core.KDFNone, "", 1, 3600, "probe")
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprintf(
			conn,
			"POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nX-Forwarded-For: 192.0.2.9\r\n%sX-Nylzo-Create: %s\r\n\r\n",
			c.header,
			harness.withIDToken(probe.createHeader(c.sizeOverride)),
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
	harness := newHarness(t, baseSettings(pepperA, nil))
	c := harness.server.HTTPServer().HTTP2
	if c == nil || c.MaxReceiveBufferPerStream < 64<<10 || c.MaxReceiveBufferPerStream > 256<<10 ||
		c.MaxReceiveBufferPerConnection < 4*c.MaxReceiveBufferPerStream ||
		c.MaxReceiveBufferPerConnection > 1<<20 {
		t.Fatalf("h2c must bound unread body per stream and leave the connection room for other streams: %+v", c)
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.BucketPerSecond = 1
	settings.Rates.BucketBurst = 2
	harness := newHarness(t, settings)
	want := map[string]string{
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
		"Permissions-Policy":           "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
	}
	check := func(name string, response testResponse, status int) {
		t.Helper()
		if response.StatusCode != status {
			t.Fatalf("%s: status %d, want %d", name, response.StatusCode, status)
		}
		for k, v := range want {
			if response.Header.Get(k) != v {
				t.Fatalf("%s: %s = %q", name, k, response.Header.Get(k))
			}
		}
		robots := "noindex, nofollow"
		if name == "page" || name == "page-head" {
			robots = ""
		}
		if response.Header.Get("X-Robots-Tag") != robots {
			t.Fatalf("%s: X-Robots-Tag = %q", name, response.Header.Get("X-Robots-Tag"))
		}
	}
	response, _ := harness.do(&testRequest{method: http.MethodGet, path: "/"})
	check("page", response, 200)
	if !strings.Contains(response.Header.Get("Content-Security-Policy"), "'nonce-") {
		t.Fatal("CSP missing on /")
	}
	response, _ = harness.do(&testRequest{method: http.MethodHead, path: "/"})
	check("page-head", response, 200)
	response, _ = harness.do(&testRequest{method: http.MethodPost, path: "/"})
	check("page-post", response, 405)
	response, _ = harness.do(
		&testRequest{
			method:  http.MethodGet,
			path:    "/api/v1/instance",
			headers: map[string]string{"Sec-Purpose": "prefetch"},
		},
	)
	check("sec-purpose", response, 503)
	response, _ = harness.do(&testRequest{method: http.MethodPost, path: "/api/v1/messages", body: []byte("x")})
	check("create-400", response, 400)
	harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	response, _ = harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	check("throttled", response, 429)
}

func TestRejectedCreateKeepsH2CConnection(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	addr := harness.listen()
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
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		return response
	}
	if response := send(
		"POST",
		"/api/v1/messages",
	); response.StatusCode != http.StatusBadRequest ||
		response.ProtoMajor != 2 {
		t.Fatalf("rejected create over h2c: %d HTTP/%d", response.StatusCode, response.ProtoMajor)
	}
	if response := send("GET", "/healthz"); response.StatusCode != http.StatusOK || response.ProtoMajor != 2 {
		t.Fatalf("follow-up over h2c: %d HTTP/%d", response.StatusCode, response.ProtoMajor)
	}
	if len(reused) != 2 || !reused[1] {
		t.Fatalf("a rejected create closed the shared h2c connection: %v", reused)
	}
}

type gatedPut struct {
	store.Blobs

	entered chan struct{}
	release chan struct{}
}

func (b *gatedPut) Put(ctx context.Context, ref string, r io.Reader, size int64) error {
	close(b.entered)
	<-b.release
	return b.Blobs.Put(ctx, ref, r, size)
}

func TestWaitingUploadDoesNotStallItsH2CConnection(t *testing.T) {
	dir := t.TempDir()
	database, err := sqlite.Open(t.Context(), filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	gated := &gatedPut{Blobs: blobs, entered: make(chan struct{}), release: make(chan struct{})}
	settings := config.Defaults()
	settings.DataDir = dir
	settings.Pepper = bytes.Repeat([]byte{1}, 32)
	s, err := New(&settings, database, gated, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := s.HTTPServer()
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &protocols}}
	base := "http://" + ln.Addr().String()

	id := core.NewID()
	token, _ := s.idTokens.Issue(id, s.clock())
	readKey, manageKey := core.PublicKey(core.NewSecret()), core.PublicKey(core.NewSecret())
	size := int64(256 << 10)
	hdr, _ := json.Marshal(
		map[string]any{
			"id":         id.String(),
			"id_token":   token,
			"read_key":   core.Encode(readKey[:]),
			"manage_key": core.Encode(manageKey[:]),
			"kdf_id":     0,
			"opens_max":  1,
			"ttl_s":      60,
			"size":       size,
		},
	)
	done := make(chan struct{})
	stalled := make(chan struct{})
	object := append(core.NewHeader(core.KDFNone).Bytes(), make([]byte, size-core.HeaderLen)...)
	body := &signalReader{r: bytes.NewReader(object), after: h2cStreamBuffer, signal: stalled}
	go func() {
		defer close(done)
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/api/v1/messages", body)
		request.ContentLength = size
		request.Header.Set("X-Nylzo-Create", core.Encode(hdr))
		if response, doErr := client.Do(request); doErr == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
	}()
	<-gated.entered
	<-stalled

	start := time.Now()
	request, _ := http.NewRequest(http.MethodPost, base+"/api/v1/abuse", strings.NewReader(`{"id":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if elapsed := time.Since(start); response.ProtoMajor != 2 || elapsed > time.Second {
		t.Fatalf("a small request waited %v (%s) behind an upload that has not been read yet", elapsed, response.Proto)
	}
	close(gated.release)
	<-done
}

type signalReader struct {
	r      io.Reader
	read   int
	after  int
	signal chan struct{}
}

func (s *signalReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if s.read <= s.after && s.read+n > s.after {
		close(s.signal)
	}
	s.read += n
	return n, err
}

type deadlineWriter struct {
	http.ResponseWriter

	read, write time.Time
}

func (w *deadlineWriter) SetReadDeadline(t time.Time) error {
	w.read = t
	return nil
}

func (w *deadlineWriter) SetWriteDeadline(t time.Time) error {
	w.write = t
	return nil
}

func TestTransfersGetTimeForTheirSize(t *testing.T) {
	t.Parallel()
	const size = 5373983
	want := time.Duration(size) * time.Second / store.MinTransferRate
	if want <= writeTimeout || store.LeaseFor(size) != want || store.LeaseFor(1000) != store.LeaseDuration {
		t.Fatalf("lease for %d bytes %v, small %v", size, store.LeaseFor(size), store.LeaseFor(1000))
	}
	s, _ := testServer(t, openStore(t), nil)
	w := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	start := time.Now()
	s.serveBlob(w, r, &store.Message{ID: "x"}, "", strings.NewReader("x"), size)
	if got := w.write.Sub(start); got < want || got > want+time.Second {
		t.Fatalf("a %d byte response may take %v, got a write deadline %v ahead", size, want, got)
	}

	settings := config.Defaults()
	settings.DataDir = t.TempDir()
	settings.Pepper = bytes.Repeat([]byte{1}, 32)
	database, err := sqlite.Open(t.Context(), filepath.Join(settings.DataDir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	blobs, err := disk.New(settings.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := New(&settings, database, blobs, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	id := core.NewID()
	readKey, manageKey := core.PublicKey(core.NewSecret()), core.PublicKey(core.NewSecret())
	hdr, _ := json.Marshal(
		map[string]any{
			"id":         id.String(),
			"id_token":   issueIDToken(cs, id),
			"read_key":   core.Encode(readKey[:]),
			"manage_key": core.Encode(manageKey[:]),
			"kdf_id":     0,
			"opens_max":  1,
			"ttl_s":      60,
			"size":       size,
		},
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(""))
	request.ContentLength = size
	request.Header.Set("Content-Length", strconv.Itoa(size))
	request.Header.Set("X-Nylzo-Create", core.Encode(hdr))
	cw := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	start = time.Now()
	cs.handleCreate(cw, request)
	if got := cw.read.Sub(start); got < want || got > want+time.Second {
		t.Fatalf("a %d byte upload may take %v, got a read deadline %v ahead", size, want, got)
	}
	if got := cw.write.Sub(start); got < want+writeTimeout || got > want+writeTimeout+time.Second {
		t.Fatalf(
			"the response to a %d byte upload got a write deadline %v ahead, want %v after the body",
			size,
			got,
			want+writeTimeout,
		)
	}

	body := append(core.NewHeader(core.KDFNone).Bytes(), make([]byte, 1000)...)
	id = core.NewID()
	hdr, _ = json.Marshal(
		map[string]any{
			"id":         id.String(),
			"id_token":   issueIDToken(cs, id),
			"read_key":   core.Encode(readKey[:]),
			"manage_key": core.Encode(manageKey[:]),
			"kdf_id":     0,
			"opens_max":  1,
			"ttl_s":      60,
			"size":       len(body),
		},
	)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(body))
	request.Header.Set("Content-Length", strconv.Itoa(len(body)))
	request.Header.Set("X-Nylzo-Create", core.Encode(hdr))
	record := httptest.NewRecorder()
	cw = &deadlineWriter{ResponseWriter: record}
	cs.handleCreate(cw, request)
	stored := time.Now()
	if record.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", record.Code, record.Body)
	}
	if got := cw.write.Sub(stored); got > writeTimeout || got < writeTimeout-time.Second {
		t.Fatalf("the 201 got a write deadline %v after the stored body, want %v", got, writeTimeout)
	}
}

func issueIDToken(s *Server, id core.ID) string {
	token, _ := s.idTokens.Issue(id, s.clock())
	return token
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func testServer(t testing.TB, database store.Store, change func(*config.Server)) (*Server, *lockedBuffer) {
	t.Helper()
	settings := config.Defaults()
	settings.DataDir = t.TempDir()
	settings.Pepper = bytes.Repeat([]byte{1}, 32)
	if change != nil {
		change(&settings)
	}
	logs := &lockedBuffer{}
	s, err := New(&settings, database, nil, WithLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	return s, logs
}

func openStore(t testing.TB) *sqlite.Store {
	t.Helper()
	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestStreamsBeyondTheLimitAnswer503BeforeAuthorization(t *testing.T) {
	t.Parallel()
	s, _ := testServer(t, openStore(t), func(c *config.Server) { c.MaxStreams = 1 })
	other := netip.MustParseAddr("198.51.100.7")
	source, ranges := s.addressKey.Source(other), s.addressKey.Range(other)
	if !s.streams.Take(source, ranges) {
		t.Fatal("the only slot was not free")
	}
	id := core.NewID().String()
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/messages/"+id+"/claim", http.NoBody),
		httptest.NewRequest(http.MethodGet, "/api/v1/messages/"+id+"/blob", http.NoBody),
	} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, request)
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "5" {
			t.Fatalf("%s with every stream slot busy: %d %q", request.URL.Path, w.Code, w.Header().Get("Retry-After"))
		}
	}
	s.streams.Release(source, ranges)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/messages/"+id+"/claim", http.NoBody))
	if w.Code != http.StatusNotFound || !s.streams.Take(source, ranges) {
		t.Fatalf("a claim with a free slot: %d, or it kept its slot", w.Code)
	}
}

func TestOneSourceCannotHoldEveryStreamSlot(t *testing.T) {
	t.Parallel()
	s, _ := testServer(t, openStore(t), func(c *config.Server) { c.MaxStreams = 64 })
	claim := func(addr string) int {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/messages/"+core.NewID().String()+"/claim", http.NoBody)
		request.RemoteAddr = addr + ":1234"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, request)
		return w.Code
	}
	hold := func(addr string) {
		a := netip.MustParseAddr(addr)
		if !s.streams.Take(s.addressKey.Source(a), s.addressKey.Range(a)) {
			t.Fatalf("no slot for %s", addr)
		}
	}
	hold("203.0.113.1")
	hold("203.0.113.1")
	if got := claim("203.0.113.1"); got != http.StatusServiceUnavailable {
		t.Fatalf("a third stream from one address: %d, want 503", got)
	}
	if got := claim("198.51.100.1"); got != http.StatusNotFound {
		t.Fatalf("another address while the first holds its share: %d, want 404 after the slot", got)
	}
	for i := 2; i <= 7; i++ {
		hold(fmt.Sprintf("203.0.113.%d", i))
	}
	if got := claim("203.0.113.9"); got != http.StatusServiceUnavailable {
		t.Fatalf("a ninth stream from one /24: %d, want 503", got)
	}
	if got := claim("2001:db8:1:2::1"); got != http.StatusNotFound {
		t.Fatalf("an IPv6 source while one /24 holds its share: %d, want 404 after the slot", got)
	}
}

func TestH2CRefusesFramesAbove16KiB(t *testing.T) {
	t.Parallel()
	s, _ := testServer(t, openStore(t), nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := s.HTTPServer()
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	frame := func(kind byte, payload []byte) []byte {
		head := make([]byte, 9)
		head[0], head[1], head[2] = byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
		head[3] = kind
		return append(head, payload...)
	}
	var out []byte
	out = append(out, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"...)
	out = append(out, frame(0x4, nil)...)
	out = append(out, frame(0xfa, make([]byte, h2cMaxFrame+1))...)
	if _, writeErr := conn.Write(out); writeErr != nil {
		t.Fatal(writeErr)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	head := make([]byte, 9)
	for {
		if _, readFullErr := io.ReadFull(r, head); readFullErr != nil {
			t.Fatalf("the server kept a connection that sent an oversized frame: %v", readFullErr)
		}
		size := int(head[0])<<16 | int(head[1])<<8 | int(head[2])
		payload := make([]byte, size)
		if _, readFullErr := io.ReadFull(r, payload); readFullErr != nil {
			t.Fatal(readFullErr)
		}
		if head[3] == 0x7 {
			if code := binary.BigEndian.Uint32(payload[4:8]); code != 0x6 {
				t.Fatalf("GOAWAY with error %#x, want FRAME_SIZE_ERROR", code)
			}
			return
		}
	}
}

func TestRunSweeperStopsWithoutAnError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, logs := testServer(t, openStore(t), func(c *config.Server) { c.SweepInterval = time.Minute })
		blobs, err := disk.New(s.settings.DataDir)
		if err != nil {
			t.Fatal(err)
		}
		s.blobs = blobs
		ctx, cancel := context.WithCancel(t.Context())
		stopped := make(chan struct{})
		go func() {
			s.RunSweeper(ctx)
			close(stopped)
		}()
		synctest.Wait()
		if n := strings.Count(logs.String(), `"msg":"sweep"`); n != 1 {
			t.Fatalf("sweeps at startup: %d", n)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if n := strings.Count(logs.String(), `"msg":"sweep"`); n != 2 {
			t.Fatalf("sweeps after one interval: %d", n)
		}
		cancel()
		synctest.Wait()
		select {
		case <-stopped:
		default:
			t.Fatal("the sweeper kept running after its context ended")
		}
		if strings.Contains(logs.String(), `"level":"ERROR"`) {
			t.Fatalf("a stopped sweeper logged an error: %s", logs.String())
		}
		stoppedEarly, stop := context.WithCancel(t.Context())
		stop()
		s.RunSweeper(stoppedEarly)
		if strings.Contains(logs.String(), `"level":"ERROR"`) {
			t.Fatalf("a sweep cut short by shutdown logged an error: %s", logs.String())
		}
	})
}

func TestConfigErrorsAreCaughtByNew(t *testing.T) {
	t.Parallel()
	database := openStore(t)
	for name, change := range map[string]func(*config.Server){
		"no PoW slot": func(c *config.Server) { c.PoWConcurrency = 0 },
	} {
		settings := config.Defaults()
		settings.Pepper = bytes.Repeat([]byte{1}, 32)
		change(&settings)
		if _, err := New(&settings, database, nil); !errors.Is(err, config.ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestClosingStopsLeaseTimersAndWaitsForRunningOnes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		pending := &timers{}
		var ran atomic.Int32
		pending.after(time.Hour, func() { ran.Add(1) })
		release := make(chan struct{})
		pending.after(time.Second, func() {
			<-release
			ran.Add(10)
		})
		time.Sleep(time.Second)
		synctest.Wait()
		stopped := make(chan struct{})
		go func() {
			pending.stop()
			close(stopped)
		}()
		synctest.Wait()
		select {
		case <-stopped:
			t.Fatal("stop returned while a timer was still running")
		default:
		}
		close(release)
		<-stopped
		pending.after(0, func() { ran.Add(100) })
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		if got := ran.Load(); got != 10 {
			t.Fatalf("timers that ran: %d, want only the one already running", got)
		}
	})
}

func TestTokenBucketThrottlesAPIPaths(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.BucketPerSecond = 1
	settings.Rates.BucketBurst = 3
	harness := newHarness(t, settings)
	codes := []int{}
	for i := range 5 {
		response, _ := harness.do(
			&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: fmt.Sprintf("203.0.%d.1", i)},
		)
		codes = append(codes, response.StatusCode)
	}
	if codes[0] != 200 || codes[2] != 200 || codes[3] != 429 || codes[4] != 429 {
		t.Fatalf("codes %v", codes)
	}
	if response, _ := harness.do(
		&testRequest{method: http.MethodGet, path: "/healthz"},
	); response.StatusCode != http.StatusOK {
		t.Fatal("non-API path throttled")
	}
	harness.clock.Advance(2 * time.Second)
	if response, _ := harness.do(
		&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: "203.0.9.1"},
	); response.StatusCode != http.StatusOK {
		t.Fatal("bucket did not refill")
	}
}

func TestSourceLimiterIsolatesClients(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.SourcePerSecond = 1
	settings.Rates.SourceBurst = 2
	harness := newHarness(t, settings)
	get := func(ip string) int {
		response, _ := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: ip})
		return response.StatusCode
	}
	if codes := []int{
		get("203.0.113.1"),
		get("203.0.113.1"),
		get("203.0.113.1"),
	}; !slices.Equal(
		codes,
		[]int{200, 200, 429},
	) {
		t.Fatalf("one source must be limited after its burst: %v", codes)
	}
	if get("2001:db8:1:1::1") != 200 || get("2001:db8:1:1::2") != 200 || get("2001:db8:1:1::3") != 429 {
		t.Fatal("addresses in one /64 must share a bucket")
	}
	if get("203.0.113.2") != 200 {
		t.Fatal("a busy source must not throttle other clients")
	}
	if response, _ := harness.do(
		&testRequest{method: http.MethodGet, path: "/healthz", address: "203.0.113.1"},
	); response.StatusCode != http.StatusOK {
		t.Fatal("non-API paths are not throttled")
	}
	harness.clock.Advance(time.Second)
	if get("203.0.113.1") != 200 {
		t.Fatal("the source bucket did not refill")
	}
}

func TestRefusedSourceTakesNoInstanceToken(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.SourcePerSecond, settings.Rates.SourceBurst = 1, 1
	settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 1, 3
	harness := newHarness(t, settings)
	get := func(ip string) int {
		response, _ := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: ip})
		return response.StatusCode
	}
	if get("203.0.113.1") != 200 {
		t.Fatal("first request")
	}
	for range 5 {
		if get("203.0.113.1") != 429 {
			t.Fatal("source not limited")
		}
	}
	if get("198.51.100.2") != 200 || get("192.0.2.3") != 200 {
		t.Fatal("a refused source drained the instance bucket")
	}
}

func TestPrefixTierLimitsOneRange(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 10, 10
	harness := newHarness(t, settings)
	codes := []int{}
	for i := range 8 {
		response, _ := harness.do(
			&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: fmt.Sprintf("2001:db8:1:%x::1", i)},
		)
		codes = append(codes, response.StatusCode)
	}
	if codes[4] != 200 || codes[5] != 429 || codes[7] != 429 {
		t.Fatalf("one /48 must get at most half of the instance bucket: %v", codes)
	}
	if response, _ := harness.do(
		&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: "2001:db8:2::1"},
	); response.StatusCode != http.StatusOK {
		t.Fatal("another /48 was throttled")
	}
}

func TestRefusedPrefixTakesNoInstanceToken(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 10, 10
	harness := newHarness(t, settings)
	for i := range 20 {
		harness.do(
			&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: fmt.Sprintf("2001:db8:1:%x::1", i)},
		)
	}
	for i := range 5 {
		if response, _ := harness.do(
			&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: fmt.Sprintf("2001:db8:%x::1", i+2)},
		); response.StatusCode != http.StatusOK {
			t.Fatalf("a refused /48 drained the instance bucket: request %d got %d", i, response.StatusCode)
		}
	}
}

func TestServerLogsHideAddressesAndSecrets(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	srv := harness.server.HTTPServer()
	if srv.ErrorLog == nil {
		t.Fatal("ErrorLog is nil: net/http would log peer addresses to stderr")
	}
	srv.ErrorLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
	s := core.NewSecret()
	id := core.NewID()
	harness.do(&testRequest{method: http.MethodGet, path: "/m/" + id.String() + "%231.0." + core.Encode(s[:])})
	logs := harness.logs.String()
	if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, core.Encode(s[:])) {
		t.Fatal("log contains a peer address or a link secret")
	}
	if !strings.Contains(logs, "net/http error") {
		t.Fatal("net/http errors must still leave a signal")
	}
}

func TestIgnoredForwardingHeaderIsLoggedOncePerHourWithoutAddresses(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.TrustedProxies = nil
	harness := newHarness(t, settings)
	harness.statusFrom(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	if strings.Contains(harness.logs.String(), "forwarded-header-ignored") {
		t.Fatal("a request without a forwarding header must not warn")
	}
	for range 3 {
		harness.statusFrom(&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: "198.51.100.7"})
	}
	if n := strings.Count(
		harness.logs.String(),
		`"msg":"forwarded-header-ignored","header":"X-Forwarded-For"`,
	); n != 1 {
		t.Fatalf("want one warning in the hour, got %d: %s", n, harness.logs.String())
	}
	harness.clock.Advance(time.Hour)
	harness.statusFrom(
		&testRequest{method: http.MethodGet, path: "/", headers: map[string]string{"Forwarded": "for=198.51.100.7"}},
	)
	logs := harness.logs.String()
	if strings.Count(logs, "forwarded-header-ignored") != 2 || !strings.Contains(logs, `"header":"Forwarded"`) {
		t.Fatalf("the next hour must warn again, for pages too: %s", logs)
	}
	if strings.Contains(logs, "198.51.100") {
		t.Fatal("the log names a client address")
	}
}

func TestUsedForwardingHeadersDoNotWarn(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	harness.statusFrom(&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: "198.51.100.7"})
	scaleway := baseSettings(pepperA, nil)
	scaleway.ProxyMode = config.ProxyModeScaleway
	scalewayHarness := newHarness(t, scaleway)
	scalewayHarness.statusFrom(
		&testRequest{
			method:  http.MethodGet,
			path:    "/api/v1/instance",
			headers: map[string]string{"X-Real-IP": "198.51.100.7", "X-Forwarded-For": "203.0.113.1"},
		},
	)
	for _, logs := range []string{harness.logs.String(), scalewayHarness.logs.String()} {
		if strings.Contains(logs, "forwarded-header-ignored") {
			t.Fatalf("a header the server used must not warn: %s", logs)
		}
	}
}

func TestForwardingHeadersThatLeaveTheProxyAddressWarn(t *testing.T) {
	t.Parallel()
	scaleway := baseSettings(pepperA, nil)
	scaleway.ProxyMode = config.ProxyModeScaleway
	for _, tc := range []struct {
		name     string
		settings config.Server
		headers  map[string]string
		header   string
	}{
		{"scaleway without X-Real-IP", *scaleway, map[string]string{"X-Forwarded-For": "203.0.113.1"}, "X-Forwarded-For"},
		{"scaleway with an unusable X-Real-IP", *scaleway, map[string]string{"X-Real-IP": "not an address"}, "X-Real-IP"},
		{"trusted proxy with only X-Real-IP", *baseSettings(pepperA, nil), map[string]string{"X-Real-IP": "203.0.113.1"}, "X-Real-IP"},
		{"trusted proxy with only Forwarded", *baseSettings(pepperA, nil), map[string]string{"Forwarded": "for=203.0.113.1"}, "Forwarded"},
		{"trusted proxy with an unreadable X-Forwarded-For", *baseSettings(pepperA, nil), map[string]string{"X-Forwarded-For": "not an address"}, "X-Forwarded-For"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t, &tc.settings)
			harness.statusFrom(&testRequest{method: http.MethodGet, path: "/api/v1/instance", headers: tc.headers})
			if !strings.Contains(harness.logs.String(), `"msg":"forwarded-header-ignored","header":"`+tc.header+`"`) {
				t.Fatalf("no warning: %s", harness.logs.String())
			}
		})
	}
}

func TestForwardingWarningRecoversFromAClockThatJumpedAhead(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.TrustedProxies = nil
	harness := newHarness(t, settings)
	harness.clock.Advance(48 * time.Hour)
	harness.statusFrom(
		&testRequest{method: http.MethodGet, path: "/", headers: map[string]string{"X-Forwarded-For": "203.0.113.1"}},
	)
	harness.clock.Advance(-47 * time.Hour)
	harness.statusFrom(
		&testRequest{method: http.MethodGet, path: "/", headers: map[string]string{"X-Forwarded-For": "203.0.113.1"}},
	)
	if n := strings.Count(harness.logs.String(), "forwarded-header-ignored"); n != 2 {
		t.Fatalf("want a warning after the clock stepped back, got %d", n)
	}
}

func TestRequestLinesLogNoSize(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "logged")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	if r, _ := harness.blob(message, response.Header.Get("X-Nylzo-Lease")); r.StatusCode != http.StatusOK {
		t.Fatalf("blob: %d", r.StatusCode)
	}
	harness.metadata(message)
	harness.status(message)
	seen := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(harness.logs.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["msg"] != "request" {
			continue
		}
		route, _ := entry["route"].(string)
		_, bytes := entry["bytes"]
		_, latency := entry["duration_ms"]
		object := route == "POST /api/v1/messages" || strings.HasSuffix(route, "/claim") ||
			strings.HasSuffix(route, "/blob")
		if bytes {
			t.Fatalf("%s logs its response size: %s", route, line)
		}
		if object && latency {
			t.Fatalf("%s logs its latency: %s", route, line)
		}
		if !object && route != "" && !latency {
			t.Fatalf("%s lost its latency: %s", route, line)
		}
		seen[route] = true
	}
	for _, r := range []string{"POST /api/v1/messages", "POST /api/v1/messages/{id}/claim", "GET /api/v1/messages/{id}/blob", "GET /api/v1/messages/{id}", "GET /api/v1/messages/{id}/status"} {
		if !seen[r] {
			t.Fatalf("no request line for %s", r)
		}
	}
}

const (
	builtIndex = `<!doctype html><html><head><link rel="icon" href="/assets/favicon-d4.svg" type="image/svg+xml"><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body></body></html>`
	appJS      = "console.log('nylzo')"
)

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
	dir := t.TempDir()
	settings.DataDir = dir
	database, err := sqlite.Open(t.Context(), filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if logs == nil {
		logs = io.Discard
	}
	srv, err := New(
		settings,
		database,
		blobs,
		WithWebFiles(files),
		WithLogger(slog.New(slog.NewJSONHandler(logs, nil))),
	)
	if err != nil {
		return nil, err
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer, nil
}

var rawClient = &http.Client{Transport: &http.Transport{DisableCompression: true}}

func get(t *testing.T, url string, header map[string]string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		request.Header.Set(k, v)
	}
	response, err := rawClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(body)
}

func pageNonce(csp string) string {
	start := strings.Index(csp, "'nonce-") + len("'nonce-")
	return csp[start : start+strings.Index(csp[start:], "'")]
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
		response, body := get(t, httpServer.URL+path, nil)
		nonce := pageNonce(response.Header.Get("Content-Security-Policy"))
		if response.StatusCode != http.StatusOK || strings.Contains(body, "placeholder") ||
			strings.Contains(body, "__NYLZO_CSP_NONCE__") || strings.Count(body, `nonce="`+nonce+`"`) != 2 {
			t.Fatalf("%s: %d %s", path, response.StatusCode, body)
		}
	}
	for path, contentType := range map[string]string{
		"/assets/index-a1.js":      "text/javascript; charset=utf-8",
		"/assets/style-b2.css":     "text/css; charset=utf-8",
		"/assets/kdf-worker-c3.js": "text/javascript; charset=utf-8",
		"/assets/favicon-d4.svg":   "image/svg+xml",
		"/assets/favicon-e5.png":   "image/png",
	} {
		response, body := get(t, httpServer.URL+path, nil)
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != contentType || body == "" {
			t.Fatalf("%s: %d %s", path, response.StatusCode, response.Header.Get("Content-Type"))
		}
		if csp := response.Header.Get("Content-Security-Policy"); csp != webclient.NonHTMLCSP {
			t.Fatalf("%s: the worker and the other assets must keep CSP default-src 'none', got %q", path, csp)
		}
		if response.Header.Get("X-Content-Type-Options") != "nosniff" ||
			response.Header.Get("Cache-Control") != "no-store" ||
			response.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s: headers %v", path, response.Header)
		}
	}
	for _, path := range []string{
		"/assets/missing.js", "/assets/", "/assets/../placeholder.html", "/assets/%2e%2e%2fplaceholder.html",
		"/assets/index-a1.js.br", "/assets/index-a1.js.gz", "/index.html",
	} {
		if response, _ := get(t, httpServer.URL+path, nil); response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", path, response.StatusCode)
		}
	}
	response, body := get(
		t,
		httpServer.URL+"/assets/index-a1.js",
		map[string]string{"Accept-Encoding": "gzip, deflate, br"},
	)
	if response.Header.Get("Content-Encoding") != "gzip" || gunzipped(t, body) != appJS {
		t.Fatalf("a gzip variant was not sent to a client that accepts it: %q", response.Header.Get("Content-Encoding"))
	}
	response, body = get(t, httpServer.URL+"/assets/style-b2.css", map[string]string{"Accept-Encoding": "br, gzip"})
	if response.Header.Get("Content-Encoding") != "" || body != "body{}" {
		t.Fatalf("an asset without compressed variants: %q %q", response.Header.Get("Content-Encoding"), body)
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
		response, body := get(t, httpServer.URL+path, nil)
		if response.StatusCode != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("%s: %d %s", path, response.StatusCode, body)
		}
		if robots := response.Header.Get("X-Robots-Tag"); (path == "/" || path == "/cs/") != (robots == "") {
			t.Fatalf("%s: X-Robots-Tag %q, only the home pages may be indexed (D82)", path, robots)
		}
	}
	noFollow := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, path := range []string{"/cs", "/cs?utm=1"} {
		request, newRequestWithContextErr := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			httpServer.URL+path,
			http.NoBody,
		)
		if newRequestWithContextErr != nil {
			t.Fatal(newRequestWithContextErr)
		}
		response, err := noFollow.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusPermanentRedirect || response.Header.Get("Location") != "/cs/" ||
			response.Header.Get("X-Robots-Tag") != "noindex, nofollow" {
			t.Fatalf("%s: %d %q %v", path, response.StatusCode, response.Header.Get("Location"), response.Header)
		}
	}
	for _, path := range []string{"/en/", "/cs/sent", "/cs/x", "/CS/"} {
		if response, _ := get(t, httpServer.URL+path, nil); response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", path, response.StatusCode)
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
		response, plain := get(t, httpServer.URL+path, nil)
		zipped, packed := get(t, httpServer.URL+path, map[string]string{"Accept-Encoding": "gzip, deflate, br"})
		if response.Header.Get("Content-Encoding") != "" || zipped.Header.Get("Content-Encoding") != "gzip" ||
			response.Header.Get("Vary") != "Accept-Encoding" || zipped.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s: encoding headers %v %v", path, response.Header, zipped.Header)
		}
		first := pageNonce(response.Header.Get("Content-Security-Policy"))
		second := pageNonce(zipped.Header.Get("Content-Security-Policy"))
		unpacked := gunzipped(t, packed)
		if first == second || strings.ReplaceAll(unpacked, second, "N") != strings.ReplaceAll(plain, first, "N") {
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
	response, body := get(t, without.URL+"/robots.txt", nil)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/plain; charset=utf-8" ||
		body != string(webclient.Robots("")) {
		t.Fatalf("robots.txt: %d %q %q", response.StatusCode, response.Header.Get("Content-Type"), body)
	}
	if response, _ := get(t, without.URL+"/sitemap.xml", nil); response.StatusCode != http.StatusNotFound {
		t.Fatalf("sitemap.xml without a public URL: %d", response.StatusCode)
	}
	settings := baseSettings(pepperA, nil)
	settings.PublicURL = "https://nylzo.example"
	settings.Version = "v1.2.3"
	with, err := newWebServerWith(t, builtClient(t), nil, settings)
	if err != nil {
		t.Fatal(err)
	}
	response, body = get(t, with.URL+"/sitemap.xml", nil)
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != "application/xml; charset=utf-8" ||
		body != string(webclient.Sitemap(settings.PublicURL)) {
		t.Fatalf("sitemap.xml: %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	response, body = get(t, with.URL+webclient.ManifestPath, nil)
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != "application/json; charset=utf-8" ||
		response.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP ||
		response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("manifest: %d %v", response.StatusCode, response.Header)
	}
	build, err := webclient.Load(builtClient(t))
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(build.Manifest("v1.2.3", settings.PublicURL))
	if err != nil {
		t.Fatal(err)
	}
	if body != string(want) {
		t.Fatalf("manifest\n got %s\nwant %s", body, want)
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
	response, body := get(t, httpServer.URL+"/", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "placeholder") ||
		strings.Contains(body, "__NYLZO_CSP_NONCE__") {
		t.Fatalf("placeholder: %d %s", response.StatusCode, body)
	}
	if response, _ := get(t, httpServer.URL+"/assets/index-a1.js", nil); response.StatusCode != http.StatusNotFound {
		t.Fatalf("an asset without a build: %d", response.StatusCode)
	}
	embedded, err := newWebServer(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response, body := get(
		t,
		embedded.URL+"/",
		nil,
	); response.StatusCode != http.StatusOK ||
		!strings.Contains(body, "<html") {
		t.Fatalf("embedded client: %d %s", response.StatusCode, body)
	}
	broken := builtClient(t)
	maps.DeleteFunc(broken, func(name string, _ *fstest.MapFile) bool { return name == "dist/home.cs.html" })
	if _, newWebServerErr := newWebServer(t, broken, nil); newWebServerErr == nil {
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
	response, body := get(t, httpServer.URL+TransparencyPath, nil)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP {
		t.Fatalf("%d %v", response.StatusCode, response.Header)
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
		if !strings.Contains(body, want) {
			t.Fatalf("transparency is missing %s: %s", want, body)
		}
	}
	plain, err := newWebServer(t, builtClient(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, body := get(t, plain.URL+TransparencyPath, nil); strings.Contains(body, "operator") {
		t.Fatalf("an unset operator must not appear: %s", body)
	}
}
