package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

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
		fmt.Fprint(c, "GET /healthz HTTP/1.1\r\nHost: nylzo\r\nX-Slow: ")
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
		line, readErr := bufio.NewReader(conn).ReadString('\n')
		conn.Close()
		if readErr != nil || !strings.Contains(line, " "+c.status+" ") {
			t.Fatalf("%s: %q %v (response must arrive before any body is sent)", c.name, line, readErr)
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

func TestRejectedCreateKeepsH2CConnection(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	addr := harness.listen()
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &protocols}, Timeout: 5 * time.Second}
	var reused []bool
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = append(reused, info.Reused) }}
	type answer struct{ status, protocol int }
	send := func(method, path string) answer {
		request, err := http.NewRequestWithContext(
			httptrace.WithClientTrace(t.Context(), trace),
			method,
			"http://"+addr+path,
			http.NoBody,
		)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		io.Copy(io.Discard, response.Body)
		return answer{status: response.StatusCode, protocol: response.ProtoMajor}
	}
	if got := send(http.MethodPost, "/api/v1/messages"); got.status != http.StatusBadRequest || got.protocol != 2 {
		t.Fatalf("rejected create over h2c: %d HTTP/%d", got.status, got.protocol)
	}
	if got := send(http.MethodGet, "/healthz"); got.status != http.StatusOK || got.protocol != 2 {
		t.Fatalf("follow-up over h2c: %d HTTP/%d", got.status, got.protocol)
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

func testServer(tb testing.TB, database store.Store, change func(*config.Server)) (*Server, *lockedBuffer) {
	tb.Helper()
	settings := config.Defaults()
	settings.DataDir = tb.TempDir()
	settings.Pepper = bytes.Repeat([]byte{1}, 32)
	if change != nil {
		change(&settings)
	}
	logs := &lockedBuffer{}
	server, err := New(&settings, database, nil, WithLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	if err != nil {
		tb.Fatal(err)
	}
	return server, logs
}

func openStore(tb testing.TB) *sqlite.Store {
	tb.Helper()
	database, err := sqlite.Open(tb.Context(), filepath.Join(tb.TempDir(), "nylzo.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { database.Close() })
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
		size := len(payload)
		return append([]byte{byte(size >> 16), byte(size >> 8), byte(size), kind, 0, 0, 0, 0, 0}, payload...)
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
		_, size := entry["bytes"]
		_, latency := entry["duration_ms"]
		object := route == "POST /api/v1/messages" || strings.HasSuffix(route, "/claim") ||
			strings.HasSuffix(route, "/blob")
		if size {
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

func TestTokenBucketsThrottleTheAPIInOrder(t *testing.T) {
	t.Parallel()
	statuses := func(harness *testHarness, addresses ...string) []int {
		var codes []int
		for _, address := range addresses {
			response, _ := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: address})
			codes = append(codes, response.StatusCode)
		}
		return codes
	}
	ok, refused := http.StatusOK, http.StatusTooManyRequests
	t.Run("instance", func(t *testing.T) {
		t.Parallel()
		settings := baseSettings(pepperA, nil)
		settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 1, 3
		harness := newHarness(t, settings)
		got := statuses(harness, "203.0.0.1", "203.0.1.1", "203.0.2.1", "203.0.3.1")
		if !slices.Equal(got, []int{ok, ok, ok, refused}) {
			t.Fatalf("the instance bucket: %v", got)
		}
		if response, _ := harness.do(
			&testRequest{method: http.MethodGet, path: "/healthz"},
		); response.StatusCode != ok {
			t.Fatal("a path outside the API was throttled")
		}
		harness.clock.Advance(2 * time.Second)
		if got = statuses(harness, "203.0.9.1"); got[0] != ok {
			t.Fatal("the instance bucket did not refill")
		}
	})
	t.Run("source", func(t *testing.T) {
		t.Parallel()
		settings := baseSettings(pepperA, nil)
		settings.Rates.SourcePerSecond, settings.Rates.SourceBurst = 1, 2
		settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 1, 6
		harness := newHarness(t, settings)
		got := statuses(harness,
			"203.0.113.1", "203.0.113.1", "203.0.113.1",
			"2001:db8:1:1::1", "2001:db8:1:1::2", "2001:db8:1:1::3",
			"203.0.113.2", "192.0.2.3",
		)
		if !slices.Equal(got, []int{ok, ok, refused, ok, ok, refused, ok, ok}) {
			t.Fatalf("a source and its /64 share a bucket and refused requests take no instance token: %v", got)
		}
		harness.clock.Advance(2 * time.Second)
		if got = statuses(harness, "203.0.113.1"); got[0] != ok {
			t.Fatal("the source bucket did not refill")
		}
	})
	t.Run("range", func(t *testing.T) {
		t.Parallel()
		settings := baseSettings(pepperA, nil)
		settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 10, 10
		harness := newHarness(t, settings)
		var addresses []string
		for i := range 8 {
			addresses = append(addresses, fmt.Sprintf("2001:db8:1:%x::1", i))
		}
		for i := range 5 {
			addresses = append(addresses, fmt.Sprintf("2001:db8:%x::1", i+2))
		}
		got := statuses(harness, addresses...)
		want := []int{ok, ok, ok, ok, ok, refused, refused, refused, ok, ok, ok, ok, ok}
		if !slices.Equal(got, want) {
			t.Fatalf("one /48 gets at most half of the instance bucket and its refusals take no token: %v", got)
		}
	})
}
