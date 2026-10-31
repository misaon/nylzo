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
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestAK22SlowConnectionsAndLyingContentLength(t *testing.T) {
	harness := newHarness(t, baseSettings(pepperA, nil))
	address := harness.listen()
	var connections []net.Conn
	t.Cleanup(func() {
		for _, connection := range connections {
			connection.Close()
		}
	})
	for range 100 {
		connection, err := net.Dial("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(connection, "GET /healthz HTTP/1.1\r\nHost: nylzo\r\nX-Slow: ")
		connections = append(connections, connection)
	}
	message := newMessage(t, core.KDFNone, "", 1, 3600, "liar")
	for i := range 100 {
		connection, err := net.Dial("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		liar := newMessage(t, core.KDFNone, "", 1, 3600, "liar")
		fmt.Fprintf(
			connection,
			"POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nX-Forwarded-For: 10.9.%d.1\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n",
			i,
			len(liar.object),
			harness.withIDToken(liar.createHeader(0)),
		)
		connection.Write(liar.object[:20])
		connections = append(connections, connection)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	response, err := client.Get("http://" + address + "/healthz")
	if err != nil {
		t.Fatalf("healthz during slowloris: %v", err)
	}
	response.Body.Close()
	// AK22 requires other requests to be answered within 2 s during a slowloris
	if response.StatusCode != http.StatusOK || time.Since(start) > 2*time.Second {
		t.Fatalf("healthz %d after %s", response.StatusCode, time.Since(start))
	}
	r, _ := http.NewRequest(
		http.MethodPost,
		"http://"+address+"/api/v1/messages",
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
	for _, check := range checks {
		connection, dialErr := net.Dial("tcp", address)
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		probe := newMessage(t, core.KDFNone, "", 1, 3600, "probe")
		connection.SetDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprintf(
			connection,
			"POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nX-Forwarded-For: 192.0.2.9\r\n%sX-Nylzo-Create: %s\r\n\r\n",
			check.header,
			harness.withIDToken(probe.createHeader(check.sizeOverride)),
		)
		line, readErr := bufio.NewReader(connection).ReadString('\n')
		connection.Close()
		if readErr != nil || !strings.Contains(line, " "+check.status+" ") {
			t.Fatalf("%s: %q %v (response must arrive before any body is sent)", check.name, line, readErr)
		}
	}
}

func TestH2CReceiveBufferIsBounded(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	http2 := harness.server.HTTPServer().HTTP2
	if http2 == nil || http2.MaxReceiveBufferPerStream < 64<<10 || http2.MaxReceiveBufferPerStream > 256<<10 ||
		http2.MaxReceiveBufferPerConnection < 4*http2.MaxReceiveBufferPerStream ||
		http2.MaxReceiveBufferPerConnection > 1<<20 {
		t.Fatalf("h2c must bound unread body per stream and leave the connection room for other streams: %+v", http2)
	}
}

func TestRejectedCreateKeepsH2CConnection(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	address := harness.listen()
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
			"http://"+address+path,
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

func (b *gatedPut) Put(ctx context.Context, ref string, body io.Reader, size int64) error {
	close(b.entered)
	<-b.release
	return b.Blobs.Put(ctx, ref, body, size)
}

func TestWaitingUploadDoesNotStallItsH2CConnection(t *testing.T) {
	directory := t.TempDir()
	database, err := sqlite.Open(t.Context(), filepath.Join(directory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	blobs, err := disk.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	gated := &gatedPut{Blobs: blobs, entered: make(chan struct{}), release: make(chan struct{})}
	settings := config.Defaults()
	settings.ReportKey = testReportKey.PublicKey()
	settings.DataDir = directory
	settings.Pepper = bytes.Repeat([]byte{1}, 32)
	server, err := New(&settings, database, gated, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := server.HTTPServer()
	go httpServer.Serve(listener)
	t.Cleanup(func() { httpServer.Close() })
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &protocols}}
	base := "http://" + listener.Addr().String()

	id := core.NewID()
	token, _ := server.idTokens.Issue(id, server.clock())
	readKey, manageKey := core.PublicKey(core.NewSecret()), core.PublicKey(core.NewSecret())
	size := int64(256 << 10)
	header, _ := json.Marshal(
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
	body := &signalReader{reader: bytes.NewReader(object), after: h2cStreamBuffer, signal: stalled}
	go func() {
		defer close(done)
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/api/v1/messages", body)
		request.ContentLength = size
		request.Header.Set("X-Nylzo-Create", core.Encode(header))
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
	reader io.Reader
	read   int
	after  int
	signal chan struct{}
}

func (s *signalReader) Read(data []byte) (int, error) {
	count, err := s.reader.Read(data)
	if s.read <= s.after && s.read+count > s.after {
		close(s.signal)
	}
	s.read += count
	return count, err
}

type deadlineWriter struct {
	http.ResponseWriter

	read, write time.Time
}

func (w *deadlineWriter) SetReadDeadline(deadline time.Time) error {
	w.read = deadline
	return nil
}

func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.write = deadline
	return nil
}

func TestTransfersGetTimeForTheirSize(t *testing.T) {
	t.Parallel()
	const size = 5373952
	want := time.Duration(size) * time.Second / store.MinTransferRate
	if want <= writeTimeout || store.LeaseFor(size) != want || store.LeaseFor(1000) != store.LeaseDuration {
		t.Fatalf("lease for %d bytes %v, small %v", size, store.LeaseFor(size), store.LeaseFor(1000))
	}
	blobServer, _ := testServer(t, openStore(t), nil)
	blobWriter := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	start := time.Now()
	blobServer.serveBlob(blobWriter, r, &store.Message{Size: size}, "", strings.NewReader("x"))
	if got := blobWriter.write.Sub(start); got < want || got > want+time.Second {
		t.Fatalf("a %d byte response may take %v, got a write deadline %v ahead", size, want, got)
	}

	settings := config.Defaults()
	settings.ReportKey = testReportKey.PublicKey()
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
	server, err := New(&settings, database, blobs, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	id := core.NewID()
	readKey, manageKey := core.PublicKey(core.NewSecret()), core.PublicKey(core.NewSecret())
	header, _ := json.Marshal(
		map[string]any{
			"id":         id.String(),
			"id_token":   issueIDToken(server, id),
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
	request.Header.Set("X-Nylzo-Create", core.Encode(header))
	writer := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	start = time.Now()
	server.handleCreate(writer, request)
	if got := writer.read.Sub(start); got < want || got > want+time.Second {
		t.Fatalf("a %d byte upload may take %v, got a read deadline %v ahead", size, want, got)
	}
	if got := writer.write.Sub(start); got < want+writeTimeout || got > want+writeTimeout+time.Second {
		t.Fatalf(
			"the response to a %d byte upload got a write deadline %v ahead, want %v after the body",
			size,
			got,
			want+writeTimeout,
		)
	}

	body := append(core.NewHeader(core.KDFNone).Bytes(), make([]byte, 1000)...)
	id = core.NewID()
	header, _ = json.Marshal(
		map[string]any{
			"id":         id.String(),
			"id_token":   issueIDToken(server, id),
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
	request.Header.Set("X-Nylzo-Create", core.Encode(header))
	record := httptest.NewRecorder()
	writer = &deadlineWriter{ResponseWriter: record}
	server.handleCreate(writer, request)
	stored := time.Now()
	if record.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", record.Code, record.Body)
	}
	if got := writer.write.Sub(stored); got > writeTimeout || got < writeTimeout-time.Second {
		t.Fatalf("the 201 got a write deadline %v after the stored body, want %v", got, writeTimeout)
	}
}

func issueIDToken(server *Server, id core.ID) string {
	token, _ := server.idTokens.Issue(id, server.clock())
	return token
}

func testServer(tb testing.TB, database store.Store, change func(*config.Server)) (*Server, *syncBuffer) {
	tb.Helper()
	settings := config.Defaults()
	settings.ReportKey = testReportKey.PublicKey()
	settings.DataDir = tb.TempDir()
	settings.Pepper = bytes.Repeat([]byte{1}, 32)
	if change != nil {
		change(&settings)
	}
	logs := &syncBuffer{}
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
	server, _ := testServer(t, openStore(t), func(settings *config.Server) { settings.MaxStreams = 1 })
	other := netip.MustParseAddr("198.51.100.7")
	source, ranges := server.addressKey.Source(other), server.addressKey.Range(other)
	if !server.streams.Take(source, ranges) {
		t.Fatal("the only slot was not free")
	}
	id := core.NewID().String()
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/messages/"+id+"/claim", http.NoBody),
		httptest.NewRequest(http.MethodGet, "/api/v1/messages/"+id+"/blob", http.NoBody),
	} {
		recorder := httptest.NewRecorder()
		server.handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "5" {
			t.Fatalf(
				"%s with every stream slot busy: %d %q",
				request.URL.Path,
				recorder.Code,
				recorder.Header().Get("Retry-After"),
			)
		}
	}
	server.streams.Release(source, ranges)
	recorder := httptest.NewRecorder()
	server.handler.
		ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/messages/"+id+"/claim", http.NoBody))
	if recorder.Code != http.StatusNotFound || !server.streams.Take(source, ranges) {
		t.Fatalf("a claim with a free slot: %d, or it kept its slot", recorder.Code)
	}
}

func TestOneSourceCannotHoldEveryStreamSlot(t *testing.T) {
	t.Parallel()
	server, _ := testServer(t, openStore(t), func(settings *config.Server) { settings.MaxStreams = 64 })
	claim := func(address string) int {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/messages/"+core.NewID().String()+"/claim", http.NoBody)
		request.RemoteAddr = address + ":1234"
		recorder := httptest.NewRecorder()
		server.handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	hold := func(address string) {
		parsed := netip.MustParseAddr(address)
		if !server.streams.Take(server.addressKey.Source(parsed), server.addressKey.Range(parsed)) {
			t.Fatalf("no slot for %s", address)
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
	server, _ := testServer(t, openStore(t), nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := server.HTTPServer()
	go httpServer.Serve(listener)
	t.Cleanup(func() { httpServer.Close() })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	frame := func(kind byte, payload []byte) []byte {
		size := len(payload)
		return append([]byte{byte(size >> 16), byte(size >> 8), byte(size), kind, 0, 0, 0, 0, 0}, payload...)
	}
	var out []byte
	out = append(out, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"...)
	out = append(out, frame(0x4, nil)...)
	out = append(out, frame(0xfa, make([]byte, h2cMaxFrame+1))...)
	if _, writeErr := connection.Write(out); writeErr != nil {
		t.Fatal(writeErr)
	}
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(connection)
	head := make([]byte, 9)
	for {
		if _, readFullErr := io.ReadFull(reader, head); readFullErr != nil {
			t.Fatalf("the server kept a connection that sent an oversized frame: %v", readFullErr)
		}
		size := int(head[0])<<16 | int(head[1])<<8 | int(head[2])
		payload := make([]byte, size)
		if _, readFullErr := io.ReadFull(reader, payload); readFullErr != nil {
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
		server, logs := testServer(
			t,
			openStore(t),
			func(settings *config.Server) { settings.SweepInterval = time.Minute },
		)
		blobs, err := disk.New(server.settings.DataDir)
		if err != nil {
			t.Fatal(err)
		}
		server.blobs = blobs
		ctx, cancel := context.WithCancel(t.Context())
		stopped := make(chan struct{})
		go func() {
			server.RunSweeper(ctx)
			close(stopped)
		}()
		synctest.Wait()
		if sweeps := strings.Count(logs.String(), `"msg":"sweep"`); sweeps != 1 {
			t.Fatalf("sweeps at startup: %d", sweeps)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if sweeps := strings.Count(logs.String(), `"msg":"sweep"`); sweeps != 2 {
			t.Fatalf("sweeps after one interval: %d", sweeps)
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
		server.RunSweeper(stoppedEarly)
		if strings.Contains(logs.String(), `"level":"ERROR"`) {
			t.Fatalf("a sweep cut short by shutdown logged an error: %s", logs.String())
		}
	})
}

func TestConfigErrorsAreCaughtByNew(t *testing.T) {
	t.Parallel()
	database := openStore(t)
	for name, change := range map[string]func(*config.Server){
		"no PoW slot": func(settings *config.Server) { settings.PoWConcurrency = 0 },
	} {
		settings := config.Defaults()
		settings.ReportKey = testReportKey.PublicKey()
		settings.Pepper = bytes.Repeat([]byte{1}, 32)
		change(&settings)
		if _, err := New(&settings, database, nil); !errors.Is(err, config.ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestStartRefusesReportsInTheClearFromBeforeTheFirstRelease(t *testing.T) {
	t.Parallel()
	database := openStore(t)
	for _, name := range []string{"abuse-reports.jsonl", filepath.Join("abuse", "2026-10-20.jsonl")} {
		settings := baseSettings(pepperA, nil)
		settings.DataDir = t.TempDir()
		path := filepath.Join(settings.DataDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		report := []byte(`{"date":"2026-10-20","id":"AAAAAAAAAAAAAAAAAAAAAA","reason":"spam"}` + "\n")
		if err := os.WriteFile(path, report, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(settings, database, nil); !errors.Is(err, abuse.ErrLegacyReports) {
			t.Fatalf("%s: %v", name, err)
		}
		if kept, err := os.ReadFile(path); err != nil || !bytes.Equal(kept, report) {
			t.Fatalf("the refused start changed %s: %v", name, err)
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
	httpServer := harness.server.HTTPServer()
	if httpServer.ErrorLog == nil {
		t.Fatal("ErrorLog is nil: net/http would log peer addresses to stderr")
	}
	httpServer.ErrorLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
	secret := core.NewSecret()
	id := core.NewID()
	harness.do(&testRequest{method: http.MethodGet, path: "/m/" + id.String() + "%231.0." + core.Encode(secret[:])})
	logs := harness.logs.String()
	if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, core.Encode(secret[:])) {
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
	if count := strings.Count(
		harness.logs.String(),
		`"msg":"forwarded-header-ignored","header":"X-Forwarded-For"`,
	); count != 1 {
		t.Fatalf("want one warning in the hour, got %d: %s", count, harness.logs.String())
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
	for _, testCase := range []struct {
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
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t, &testCase.settings)
			harness.statusFrom(
				&testRequest{method: http.MethodGet, path: "/api/v1/instance", headers: testCase.headers},
			)
			if !strings.Contains(
				harness.logs.String(),
				`"msg":"forwarded-header-ignored","header":"`+testCase.header+`"`,
			) {
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
	if count := strings.Count(harness.logs.String(), "forwarded-header-ignored"); count != 2 {
		t.Fatalf("want a warning after the clock stepped back, got %d", count)
	}
}

func TestRequestLinesLogNoSize(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "logged")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	if blobResponse, _ := harness.blob(
		message,
		response.Header.Get("X-Nylzo-Lease"),
	); blobResponse.StatusCode != http.StatusOK {
		t.Fatalf("blob: %d", blobResponse.StatusCode)
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
	for _, requestLine := range []string{"POST /api/v1/messages", "POST /api/v1/messages/{id}/claim", "GET /api/v1/messages/{id}/blob", "GET /api/v1/messages/{id}", "GET /api/v1/messages/{id}/status"} {
		if !seen[requestLine] {
			t.Fatalf("no request line for %s", requestLine)
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
