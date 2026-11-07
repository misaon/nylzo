package server

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
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
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/transport"
	"github.com/misaon/nylzo/internal/store"
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
	tooLarge := harness.settings.Limits.MaxObjectBytes + envelope.Pad
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
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		http2 := harness.server.HTTPServer().HTTP2
		if http2 == nil || http2.MaxReceiveBufferPerStream < 64<<10 || http2.MaxReceiveBufferPerStream > 256<<10 ||
			http2.MaxReceiveBufferPerConnection < 4*http2.MaxReceiveBufferPerStream ||
			http2.MaxReceiveBufferPerConnection > 1<<20 {
			t.Fatalf(
				"h2c must bound unread body per stream and leave the connection room for other streams: %+v",
				http2,
			)
		}
	})
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

func (b *gatedPut) PutExpiring(
	ctx context.Context,
	ref string,
	body io.Reader,
	size int64,
	ttl time.Duration,
) (time.Time, error) {
	close(b.entered)
	<-b.release
	return b.Blobs.PutExpiring(ctx, ref, body, size, ttl)
}

func TestWaitingUploadDoesNotStallItsH2CConnection(t *testing.T) {
	harness := newHarness(t, baseSettings(pepperA, nil))
	gated := &gatedPut{Blobs: harness.blobs, entered: make(chan struct{}), release: make(chan struct{})}
	harness.blobs = gated
	harness.reopen(&harness.settings)
	server := harness.server
	address := harness.listen()
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &protocols}}
	base := "http://" + address

	id := core.NewID()
	token := issueIDToken(server, id)
	readKey, manageKey := signing.PublicKey(core.NewSecret()), signing.PublicKey(core.NewSecret())
	size := int64(256 << 10)
	header, _ := json.Marshal(
		map[string]any{
			"id":           id.String(),
			"id_token":     token,
			"read_key":     core.Encode(readKey[:]),
			"manage_key":   core.Encode(manageKey[:]),
			"verifier_key": otherPublicKey(),
			"epoch":        shareEpochFor(60),
			"kdf_id":       0,
			"opens_max":    1,
			"ttl_s":        60,
			"size":         size,
		},
	)
	done := make(chan struct{})
	stalled := make(chan struct{})
	object := append(envelope.NewHeader(core.KDFNone).Bytes(), make([]byte, size-envelope.HeaderLen)...)
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

	read, write   time.Time
	reads, writes []time.Time
}

func (w *deadlineWriter) SetReadDeadline(deadline time.Time) error {
	if w.reads == nil {
		w.read = deadline
	}
	w.reads = append(w.reads, deadline)
	return nil
}

func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	if w.writes == nil {
		w.write = deadline
	}
	w.writes = append(w.writes, deadline)
	return nil
}

func TestTransfersGetTimeForTheirSize(t *testing.T) {
	t.Parallel()
	const size = 5373952
	want := time.Duration(size) * time.Second / store.MinTransferRate
	if want <= writeTimeout || store.LeaseFor(size) != want || store.LeaseFor(1000) != store.LeaseDuration {
		t.Fatalf("lease for %d bytes %v, small %v", size, store.LeaseFor(size), store.LeaseFor(1000))
	}
	server := newHarness(t, baseSettings(pepperA, nil)).server
	blobWriter := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	start := time.Now()
	server.serveBlob(blobWriter, r, &store.Message{Size: size}, servedHeaders{}, bytes.NewReader(make([]byte, size)))
	if got := blobWriter.write.Sub(start); got < want || got > want+time.Second {
		t.Fatalf("a %d byte response may take %v, got a write deadline %v ahead", size, want, got)
	}
	window := transport.DefaultPace().Window
	if chunks := len(blobWriter.writes) - 1; chunks < size/transport.PaceChunk ||
		slices.ContainsFunc(blobWriter.writes[1:], func(deadline time.Time) bool {
			return time.Until(deadline) > window
		}) {
		t.Fatalf("F3: every 64 KiB of a response must go out within %v, so a reader that stops frees its slot", window)
	}

	id := core.NewID()
	readKey, manageKey := signing.PublicKey(core.NewSecret()), signing.PublicKey(core.NewSecret())
	header, _ := json.Marshal(
		map[string]any{
			"id":           id.String(),
			"id_token":     issueIDToken(server, id),
			"read_key":     core.Encode(readKey[:]),
			"manage_key":   core.Encode(manageKey[:]),
			"verifier_key": otherPublicKey(),
			"epoch":        shareEpochFor(60),
			"kdf_id":       0,
			"opens_max":    1,
			"ttl_s":        60,
			"size":         size,
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

	body := append(envelope.NewHeader(core.KDFNone).Bytes(), make([]byte, envelope.Pad-envelope.HeaderLen)...)
	id = core.NewID()
	header, _ = json.Marshal(
		map[string]any{
			"id":           id.String(),
			"id_token":     issueIDToken(server, id),
			"read_key":     core.Encode(readKey[:]),
			"manage_key":   core.Encode(manageKey[:]),
			"verifier_key": otherPublicKey(),
			"epoch":        shareEpochFor(60),
			"kdf_id":       0,
			"opens_max":    1,
			"ttl_s":        60,
			"size":         len(body),
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
	if got := writer.writes[len(writer.writes)-1].Sub(stored); got > writeTimeout || got < writeTimeout-time.Second {
		t.Fatalf("the 201 got a write deadline %v after the stored body, want %v", got, writeTimeout)
	}
	if len(writer.reads) < 2 || writer.reads[1].Sub(stored) > transport.DefaultPace().Window {
		t.Fatalf("F7: the body must arrive at the minimum rate, chunk by chunk: %v", writer.reads)
	}
}

func issueIDToken(server *Server, id core.ID) string {
	token, _ := server.idTokens.Issue(id, shareEpochFor(60), server.epochs.ID(), time.Now())
	return token
}

func otherPublicKey() string {
	key := signing.PublicKey(core.NewSecret())
	return core.Encode(key[:])
}

func TestStreamsBeyondTheLimitAnswer503BeforeAuthorization(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.MaxStreams = 1
	server := newHarness(t, settings).server
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
	settings := baseSettings(pepperA, nil)
	settings.MaxStreams = 64
	server := newHarness(t, settings).server
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
	for i := 2; i <= 3; i++ {
		hold(fmt.Sprintf("203.0.113.%d", i))
	}
	if got := claim("203.0.113.9"); got != http.StatusServiceUnavailable {
		t.Fatalf("F3: a fifth stream from one /24: %d, want 503", got)
	}
	if got := claim("2001:db8:1:2::1"); got != http.StatusNotFound {
		t.Fatalf("an IPv6 source while one /24 holds its share: %d, want 404 after the slot", got)
	}
}

func TestH2CRefusesFramesAbove16KiB(t *testing.T) {
	t.Parallel()
	connection, err := net.Dial("tcp", newHarness(t, baseSettings(pepperA, nil)).listen())
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

// TestRunSweeperStopsWithoutAnError also covers spec §10: a successful sweep writes no line, so no count of
// expiries, purges or failed uploads reaches the log.
func TestRunSweeperStopsWithoutAnError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.SweepInterval = time.Minute
		harness := newHarness(t, settings)
		server, logs := harness.server, harness.logs
		dueAtStartup := newMessage(t, core.KDFNone, "", 1, 60, "due at startup")
		harness.mustCreate(dueAtStartup)
		synctest.Sleep(store.BlobPurgeStep + time.Minute)
		startupLogs := logs.String()
		ctx, cancel := context.WithCancel(t.Context())
		stopped := make(chan struct{})
		go func() {
			server.RunSweeper(ctx)
			close(stopped)
		}()
		synctest.Wait()
		if harness.blobExists(dueAtStartup) {
			t.Fatal("no sweep at startup")
		}
		dueLater := newMessage(t, core.KDFNone, "", 1, 60, "due later")
		harness.mustCreate(dueLater)
		synctest.Sleep(store.BlobPurgeStep)
		if harness.blobExists(dueLater) {
			t.Fatal("no sweep after an interval")
		}
		if logs.String() != startupLogs {
			t.Fatalf("a successful sweep wrote a line: %s", strings.TrimPrefix(logs.String(), startupLogs))
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
	database := openStore(t, t.TempDir())
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
	database := openStore(t, t.TempDir())
	for _, name := range []string{"abuse-reports.jsonl", filepath.Join("abuse", "2026-10-20.jsonl")} {
		settings := baseSettings(pepperA, nil)
		settings.DataDirectory = t.TempDir()
		path := filepath.Join(settings.DataDirectory, name)
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

func TestServerLogsHideAddressesAndSecrets(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		httpServer := harness.server.HTTPServer()
		if httpServer.ErrorLog == nil {
			t.Fatal("ErrorLog is nil: net/http would log peer addresses to stderr")
		}
		httpServer.ErrorLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
		secret := core.NewSecret()
		id := core.NewID()
		harness.do(&testRequest{method: http.MethodGet, path: "/m/" + id.String() + "%231.0." + core.Encode(secret[:])})
		harness.server.flushCounts(t.Context())
		logs := harness.logs.String()
		if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, core.Encode(secret[:])) {
			t.Fatal("log contains a peer address or a link secret")
		}
		if !strings.Contains(logs, "net/http error") {
			t.Fatal("net/http errors must still leave a signal")
		}
	})
}

func TestIgnoredForwardingHeaderIsCountedWithoutAddresses(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.TrustedProxies = nil
		harness := newHarness(t, settings)
		harness.statusFrom(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
		harness.server.flushCounts(t.Context())
		if strings.Contains(harness.logs.String(), "forwarded-header-ignored") {
			t.Fatal("a request without a forwarding header must not warn")
		}
		for range 3 {
			harness.statusFrom(&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: "198.51.100.7"})
		}
		harness.server.flushCounts(t.Context())
		if count := strings.Count(
			harness.logs.String(),
			`"msg":"forwarded-header-ignored","category":"X-Forwarded-For","count":3`,
		); count != 1 {
			t.Fatalf("want one warning with the count of the window, got %d: %s", count, harness.logs.String())
		}
		harness.statusFrom(
			&testRequest{
				method:  http.MethodGet,
				path:    "/",
				headers: map[string]string{"Forwarded": "for=198.51.100.7"},
			},
		)
		harness.server.flushCounts(t.Context())
		logs := harness.logs.String()
		if strings.Count(logs, "forwarded-header-ignored") != 2 || !strings.Contains(logs, `"category":"Forwarded"`) {
			t.Fatalf("the next window must warn again, for pages too: %s", logs)
		}
		if strings.Contains(logs, "198.51.100") {
			t.Fatal("the log names a client address")
		}
	})
}

func TestUsedForwardingHeadersDoNotWarn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
		harness.server.flushCounts(t.Context())
		scalewayHarness.server.flushCounts(t.Context())
		for _, logs := range []string{harness.logs.String(), scalewayHarness.logs.String()} {
			if strings.Contains(logs, "forwarded-header-ignored") {
				t.Fatalf("a header the server used must not warn: %s", logs)
			}
		}
	})
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
			synctest.Test(t, func(t *testing.T) {
				harness := newHarness(t, &testCase.settings)
				harness.statusFrom(
					&testRequest{method: http.MethodGet, path: "/api/v1/instance", headers: testCase.headers},
				)
				harness.server.flushCounts(t.Context())
				if !strings.Contains(
					harness.logs.String(),
					`"msg":"forwarded-header-ignored","category":"`+testCase.header+`"`,
				) {
					t.Fatalf("no warning: %s", harness.logs.String())
				}
			})
		})
	}
}

// TestRequestsAreLoggedAsCountsPerHour covers A4-5, A4-8 and spec §10: no line per request, no latency, no client
// method, no line before the hour ends, and the routes that act on a message counted together by status class.
func TestRequestsAreLoggedAsCountsPerHour(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		housekeeping, stop := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			harness.server.RunHousekeeping(housekeeping)
			close(done)
		}()
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
		harness.metadata(message)
		harness.statusFrom(&testRequest{method: "AAAAAAAAAAAAAAAAAAAAAAAAAAAA", path: "/api/v1/instance"})
		hourEnd := time.Now().Truncate(time.Hour).Add(time.Hour)
		synctest.Sleep(time.Until(hourEnd) - time.Minute)
		if strings.Contains(harness.logs.String(), `"msg":"requests"`) {
			t.Fatalf("a request was logged before the hour ended: %s", harness.logs.String())
		}
		synctest.Sleep(time.Minute)
		stop()
		<-done
		counts := map[string]float64{}
		for line := range strings.SplitSeq(strings.TrimSpace(harness.logs.String()), "\n") {
			var entry map[string]any
			if json.Unmarshal([]byte(line), &entry) != nil || entry["msg"] != "requests" {
				continue
			}
			for _, leak := range []string{"duration_ms", "bytes", "id", "addr"} {
				if _, found := entry[leak]; found {
					t.Fatalf("a request count carries %s: %s", leak, line)
				}
			}
			route, _ := entry["route"].(string)
			count, _ := entry["count"].(float64)
			if class, grouped := entry["status_class"].(string); grouped {
				if _, found := entry["method"]; found {
					t.Fatalf("a count of the message routes names a method: %s", line)
				}
				counts[route+" "+class] += count
				continue
			}
			method, _ := entry["method"].(string)
			status, _ := entry["status"].(float64)
			counts[method+" "+route+" "+strconv.Itoa(int(status))] += count
		}
		if counts["message 2xx"] != 4 || counts["POST POST /api/v1/messages 201"] != 1 ||
			counts["OTHER /api/ 404"] != 1 || strings.Contains(harness.logs.String(), "AAAAAAAAAAAAAAAAAAAAAA") ||
			strings.Contains(harness.logs.String(), "{id}") {
			t.Fatalf("counts per method, route and status, the message routes by class only: %v", counts)
		}
	})
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
		synctest.Test(t, func(t *testing.T) {
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
			synctest.Sleep(2 * time.Second)
			if got = statuses(harness, "203.0.9.1"); got[0] != ok {
				t.Fatal("the instance bucket did not refill")
			}
		})
	})
	t.Run("source", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			settings := baseSettings(pepperA, nil)
			settings.Rates.SourcePerSecond, settings.Rates.SourceBurst = 1, 2
			settings.Rates.BucketPerSecond, settings.Rates.BucketBurst = 1, 100
			harness := newHarness(t, settings)
			got := statuses(harness,
				"203.0.113.1", "203.0.113.1", "203.0.113.1",
				"2001:db8:1:1::1", "2001:db8:1:1::2", "2001:db8:1:1::3",
				"203.0.113.2", "192.0.2.3",
			)
			if !slices.Equal(got, []int{ok, ok, refused, ok, ok, refused, ok, ok}) {
				t.Fatalf("a source and its /64 share a bucket and refused requests take no instance token: %v", got)
			}
			synctest.Sleep(2 * time.Second)
			if got = statuses(harness, "203.0.113.1"); got[0] != ok {
				t.Fatal("the source bucket did not refill")
			}
		})
	})
	t.Run("range", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
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
	})
}

func TestBrowserMutationGuardPrecedesChallengeSpendAndBodyReads(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		request := httptest.NewRequest(http.MethodPost, "/api/v1/messages", &unreadableBody{t: t})
		challenge, _ := harness.server.challenges.Issue(time.Now())
		seed := [core.SeedLen]byte{1}
		signature := signing.SignRequest(&seed, signing.PurposeRead, request.Method, request.URL.Path, challenge, nil)
		request.Header.Set("Authorization", signature.Authorization())
		request.Header.Set("Origin", "https://attacker.example")
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
		response := httptest.NewRecorder()
		harness.server.handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden ||
			strings.TrimSpace(response.Body.String()) != `{"error":"cross_origin"}` {
			t.Fatalf("unexpected denial: %d %s", response.Code, response.Body.String())
		}
		spent, err := harness.server.challenges.Spend(t.Context(), challenge, time.Now())
		if err != nil || !spent {
			t.Fatalf("cross-origin request consumed a challenge: first spend=%v, err=%v", spent, err)
		}
	})
}

type unreadableBody struct{ t *testing.T }

func (body *unreadableBody) Read([]byte) (int, error) {
	body.t.Error("the rejected cross-origin request body was read")
	return 0, io.EOF
}

// TestABodyThatNeverArrivesIsAnsweredAtOnce covers F5: an early answer closes instead of waiting for the body.
func TestABodyThatNeverArrivesIsAnsweredAtOnce(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	address := harness.listen()
	id := core.NewID().String()
	for _, request := range []string{
		"POST /api/v1/abuse HTTP/1.1\r\nContent-Type: text/plain\r\n",
		"POST /api/v1/messages/" + id + "/burn HTTP/1.1\r\n",
	} {
		connection, err := new(net.Dialer).DialContext(t.Context(), "tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		fmt.Fprintf(
			connection,
			"%sHost: nylzo.test\r\nX-Requested-With: XMLHttpRequest\r\nContent-Length: 4000\r\n\r\n",
			request,
		)
		connection.SetReadDeadline(start.Add(5 * time.Second))
		reader := bufio.NewReader(connection)
		response, err := http.ReadResponse(reader, nil)
		if err != nil || response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%q: %v %v", request, response, err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if _, err = reader.ReadByte(); !errors.Is(err, io.EOF) || time.Since(start) > time.Second {
			t.Fatalf(
				"%q: the connection must close at once, not wait for the body: %v after %v",
				request,
				err,
				time.Since(start),
			)
		}
		connection.Close()
	}
}

// TestHeadersOver8KiBGet431WithTheSecurityHeaders covers A4-9 and F10: net/http answers before any handler runs.
func TestHeadersOver8KiBGet431WithTheSecurityHeaders(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	address := harness.listen()
	for padding, want := range map[int]int{7 << 10: http.StatusOK, 9 << 10: http.StatusRequestHeaderFieldsTooLarge} {
		connection, err := new(net.Dialer).DialContext(t.Context(), "tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(
			connection,
			"GET /healthz HTTP/1.1\r\nHost: nylzo.test\r\nX-Padding: %s\r\n\r\n",
			strings.Repeat("a", padding),
		)
		response, err := http.ReadResponse(bufio.NewReader(connection), nil)
		connection.Close()
		if err != nil || response.StatusCode != want {
			t.Fatalf("a %d-byte header: %v %v, want %d", padding, response, err, want)
		}
		response.Body.Close()
		if response.Header.Get("X-Content-Type-Options") != "nosniff" ||
			response.Header.Get("Content-Security-Policy") == "" ||
			response.Header.Get("Strict-Transport-Security") == "" {
			t.Fatalf("a %d-byte header got a response without the security headers: %v", padding, response.Header)
		}
	}
}

// openWithShares tries every share a seed directory can still make, as an attacker with the link would.
func openWithShares(t *testing.T, directory string, link *core.Link, policy envelope.Policy, object []byte) bool {
	t.Helper()
	ratchet, _, err := epoch.Open(directory, time.Now(), epoch.Options{})
	if err != nil || ratchet == nil {
		t.Fatalf("open the stolen seed: %v", err)
	}
	defer ratchet.Close()
	locator := seal.LocatorOf(link.ID[:])
	now := epoch.Hour(time.Now())
	for hour := now; hour < now+200; hour++ {
		expiryShare, shareErr := ratchet.Share(ratchet.ID(), hour, locator, time.Now())
		if shareErr != nil {
			t.Fatal(shareErr)
		}
		derived, deriveErr := keys.DeriveAll(link.ID, &link.Secret, link.KDF, "", &expiryShare)
		if deriveErr != nil {
			t.Fatal(deriveErr)
		}
		expected := envelope.Expected{
			ID:         link.ID,
			Policy:     policy,
			KDF:        link.KDF,
			ObjectHash: &link.ObjectHash,
		}
		if _, _, openErr := envelope.Open(&derived.Content, &expected, object); openErr == nil {
			return true
		}
	}
	return false
}

func seedCopy(t *testing.T, seed []byte) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, epoch.FileName), seed, 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

// TestF3ACopyTakenWhileLiveStaysClosedOnceItsHourHasPassed copies the data directory and the object while a
// message lives, lets it expire and the epoch advance, and shows that nothing the attacker can steal then,
// the link included, opens the copy (spec section 5.6).
func TestF3ACopyTakenWhileLiveStaysClosedOnceItsHourHasPassed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.EpochDirectoryOverride = t.TempDir()
		harness := newHarness(t, settings)
		message := harness.issueMessage(core.KDFNone, "", 2, 600, "plaintext expiring sentinel")
		harness.mustCreate(message)
		response, object := harness.claim(message, message.verifier, "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		policy, err := decodePolicy(response.Header.Get(api.PolicyHeaderName))
		if err != nil {
			t.Fatal(err)
		}
		dataCopy := harness.stolenCopy()
		seedPath := filepath.Join(settings.EpochDirectoryOverride, epoch.FileName)
		liveSeed, err := os.ReadFile(seedPath)
		if err != nil {
			t.Fatal(err)
		}
		link, err := core.ParseLink(message.linkText())
		if err != nil {
			t.Fatal(err)
		}
		if !openWithShares(t, seedCopy(t, liveSeed), &link, policy, object) {
			t.Fatal("the seed of a live message must open it; the test would prove nothing")
		}

		synctest.Sleep(time.Until(epoch.End(message.epoch)) + time.Minute)
		if _, err = harness.server.sweepOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		if response, _ = harness.claim(message, message.verifier, ""); response.StatusCode != http.StatusGone {
			t.Fatalf("a claim after the expiry: %d", response.StatusCode)
		}
		currentSeed, err := os.ReadFile(seedPath)
		if err != nil {
			t.Fatal(err)
		}
		oldSeed := liveSeed[epoch.SeedOffset : epoch.SeedOffset+epoch.SeedLen]
		if bytes.Contains(currentSeed, oldSeed) {
			t.Fatal("the seed file still holds the seed of the hour that has passed")
		}
		for where, data := range dataCopy {
			if bytes.Contains(data, oldSeed) {
				t.Fatalf("the copy of the data directory holds the seed in %s", where)
			}
		}
		if openWithShares(t, seedCopy(t, currentSeed), &link, policy, object) {
			t.Fatal("the seed stolen after the expiry hour opened a copy taken while the message lived")
		}
		missing := envelope.Expected{
			ID:         link.ID,
			Policy:     policy,
			KDF:        link.KDF,
			ObjectHash: &link.ObjectHash,
		}
		withoutShare, err := keys.DeriveAll(link.ID, &link.Secret, link.KDF, "", new([core.ShareLen]byte))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = envelope.Open(&withoutShare.Content, &missing, object); err == nil {
			t.Fatal("the link alone opened the copy")
		}
	})
}

type stolenNeedles struct {
	t     *testing.T
	named map[string][]byte
}

func (n *stolenNeedles) secret(name string, value []byte) {
	n.named[name] = value
	n.named[name+" base64url"] = []byte(core.Encode(value))
	n.named[name+" hex"] = []byte(hex.EncodeToString(value))
}

func (n *stolenNeedles) text(name, value string) {
	n.named[name] = []byte(value)
}

func (n *stolenNeedles) addMessage(label string, message *testMessage, passphrase string) {
	n.t.Helper()
	n.secret(label+" S", message.secret[:])
	n.secret(label+" R", message.link.R[:])
	n.secret(label+" K", message.content.K[:])
	n.secret(label+" K_c", message.content.Commitment[:])
	n.secret(label+" E the sender sealed with", message.share[:])
	hash := envelope.ObjectHash(message.object)
	n.secret(label+" H", hash[:])
	n.secret(label+" commitment", message.object[envelope.HeaderLen:envelope.PrefixLen])
	n.secret(label+" V", message.verifierKey[:])
	n.secret(label+" M", message.manage[:])
	readKey, verifierKey := signing.PublicKey(&message.link.R), signing.PublicKey(message.verifierKey)
	manageKey := signing.PublicKey(message.manage)
	n.secret(label+" pk_R", readKey[:])
	n.secret(label+" pk_V", verifierKey[:])
	n.secret(label+" pk_M", manageKey[:])
	if passphrase != "" {
		n.secret(label+" P", testPassphraseKey(message.kdf, passphrase, &message.link.PassphraseSalt))
		n.text(label+" passphrase", passphrase)
	}
	if message.text != "" {
		n.text(label+" plaintext", message.text)
	}
}

func fileMessage(t *testing.T, content []byte) *testMessage {
	t.Helper()
	message := newMessage(t, core.KDFNone, "", 1, 3600, "unused")
	message.text = ""
	object, err := envelope.Seal(
		&message.content,
		envelope.NewHeader(core.KDFNone),
		message.id,
		message.policy,
		envelope.Payload{
			Meta: envelope.Meta{
				File: &envelope.FileMeta{
					Name: "quarterly-plan-sentinel.pdf",
					MIME: "application/pdf",
					Size: int64(len(content)),
				},
			},
			File: content,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message.object = object
	return message
}

// TestAnEndedMessageStaysUnreadableWithItsLinkAndAnOlderRow covers A4-1: the file is kept open to read its own blocks.
func TestAnEndedMessageStaysUnreadableWithItsLinkAndAnOlderRow(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	for _, ending := range []string{"burn", "ack"} {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "ending sentinel "+ending)
		harness.mustCreate(message)
		older, err := harness.store.Load(t.Context(), seal.LocatorOf(message.id[:]))
		if err != nil {
			t.Fatal(err)
		}
		stored, err := os.Open(filepath.Join(harness.directory, "blobs", hex.EncodeToString([]byte(older.BlobRef))))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stored.Close() })
		switch ending {
		case "burn":
			if response, _ := harness.post(
				message,
				"burn",
				message.readToken,
			); response.StatusCode != http.StatusNoContent {
				t.Fatalf("burn: %d", response.StatusCode)
			}
		default:
			claimed, _ := harness.claim(message, "", message.address)
			if response := harness.ack(
				message,
				claimed.Header.Get("X-Nylzo-Lease"),
			); response.StatusCode != http.StatusNoContent {
				t.Fatalf("ack: %d", response.StatusCode)
			}
		}
		readKey := signing.PublicKey(&message.link.R)
		record, err := openRow(&older, seal.Read, readKey[:])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = record.BlobKey(); err != nil {
			t.Fatalf("%s: the older row must still give the blob key, as a remnant on the disk would: %v", ending, err)
		}
		if _, err = seal.ReadEraseBlock(stored); !errors.Is(err, seal.ErrErased) {
			t.Fatalf("%s: the erase block survived in the blob's own blocks, so the link opens it: %v", ending, err)
		}
	}
}

func TestStolenServerRevealsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.Rates.GlobalDailyCreates = 18
		settings.PoWDifficulty = 1
		harness := newHarness(t, settings)
		faulty := harness.withFaultyBlobs()
		startupLogs := harness.logs.String()
		needles := &stolenNeedles{t: t, named: map[string][]byte{}}
		const ip4, ip6 = "203.0.113.77", "2001:db8:77::1"
		needles.text("IPv4 address", ip4)
		needles.text("IPv6 address", ip6)
		needles.text("IPv6 address, expanded", netip.MustParseAddr(ip6).StringExpanded())
		raw6 := netip.MustParseAddr(ip6).As16()
		needles.named["IPv6 address, raw"] = raw6[:]
		var lease string
		claimed := func(message *testMessage, verifier, ip string) {
			t.Helper()
			response, _ := harness.claim(message, verifier, ip)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("claim: %d", response.StatusCode)
			}
			lease = response.Header.Get("X-Nylzo-Lease")
			raw, err := core.DecodeExact(lease, core.SeedLen)
			if err != nil {
				t.Fatal(err)
			}
			needles.secret("lease of "+message.id.String(), raw)
			expiryShare := unwrapShare(t, response)
			needles.secret("E of "+message.id.String(), expiryShare[:])
		}

		expired := newMessage(t, core.KDFNone, "", 1, 60, "plaintext expired sentinel")
		expired.address = ip4
		harness.mustCreate(expired)
		needles.addMessage("expired", expired, "")
		synctest.Sleep(2*time.Minute + 17*time.Second)
		if _, metadata := harness.metadata(expired); metadata["state"] != "expired" {
			t.Fatalf("expired: %v", metadata)
		}

		opened := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext opened sentinel")
		opened.address = ip4
		harness.mustCreate(opened)
		needles.addMessage("opened", opened, "")
		claimed(opened, "", ip4)
		if response := harness.ack(opened, lease); response.StatusCode != http.StatusNoContent {
			t.Fatalf("ack: %d", response.StatusCode)
		}

		const passphrase = "correct horse battery staple"
		leased := newMessage(t, core.KDFScrypt, passphrase, 2, 3600, "plaintext leased sentinel")
		leased.address = ip6
		harness.mustCreate(leased)
		needles.addMessage("leased", leased, passphrase)
		harness.claim(leased, core.Encode(make([]byte, 32)), ip6)
		claimed(leased, leased.verifier, ip6)

		content := bytes.Repeat([]byte("file-content-sentinel "), 3000)
		file := fileMessage(t, content)
		file.address = ip4
		harness.mustCreate(file)
		needles.addMessage("file", file, "")
		needles.text("file name", "quarterly-plan-sentinel.pdf")
		needles.named["file content"] = content[:64]

		burned := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext burned sentinel")
		burned.address = ip4
		harness.mustCreate(burned)
		needles.addMessage("burned", burned, "")
		if response, _ := harness.post(burned, "burn", burned.readToken); response.StatusCode != http.StatusNoContent {
			t.Fatalf("burn: %d", response.StatusCode)
		}

		cancelled := newMessage(t, core.KDFScrypt, passphrase, 1, 3600, "plaintext cancelled sentinel")
		cancelled.address = ip6
		harness.mustCreate(cancelled)
		needles.addMessage("cancelled", cancelled, passphrase)
		if response, _ := harness.post(
			cancelled,
			"cancel",
			cancelled.manageToken,
		); response.StatusCode != http.StatusNoContent {
			t.Fatalf("cancel: %d", response.StatusCode)
		}

		stuck := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext stuck sentinel")
		stuck.address = ip4
		harness.mustCreate(stuck)
		needles.addMessage("stuck", stuck, "")
		faulty.FailDelete(harness.blobRef(stuck), &fs.PathError{
			Op:   "remove",
			Path: filepath.Join(harness.directory, "blobs", hex.EncodeToString([]byte(harness.blobRef(stuck)))),
			Err:  syscall.EROFS,
		})
		if response, _ := harness.post(
			stuck,
			"burn",
			stuck.readToken,
		); response.StatusCode != http.StatusInternalServerError {
			t.Fatalf("burn with a failing blob store: %d", response.StatusCode)
		}

		removed := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext removed sentinel")
		removed.address = ip4
		harness.mustCreate(removed)
		needles.addMessage("removed", removed, "")
		if _, err := store.TakeDown(
			t.Context(),
			harness.store,
			harness.blobs,
			seal.LocatorOf(removed.id[:]),
			time.Now(),
		); err != nil {
			t.Fatal(err)
		}

		pow := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext pow sentinel")
		pow.address = ip6
		filler := newMessage(t, core.KDFNone, "", 1, 3600, "filler")
		filler.address = ip4
		harness.mustCreate(filler)
		_, instance := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
		powInfo := decodeJSON(t, instance)["pow"].(map[string]any)
		if powInfo["required"] != true {
			t.Fatalf("PoW is not required yet: %v", powInfo)
		}
		token := solveWork(t, powInfo["challenge"].(string), pow.id, 1)
		if response, body := harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages",
				body:    pow.object,
				address: pow.address,
				headers: map[string]string{
					"Content-Type":   "application/octet-stream",
					"X-Nylzo-Create": pow.createHeader(0),
					"X-Nylzo-Pow":    token,
				},
			},
		); response.StatusCode != http.StatusCreated {
			t.Fatalf("create with PoW: %d %s", response.StatusCode, body)
		}
		needles.addMessage("pow", pow, "")

		link := opened.linkText()
		contact := "notifier.contact@example.cz"
		report := map[string]any{
			"id":         leased.id.String(),
			"reason":     "spam, see " + link,
			"contact":    contact,
			"good_faith": true,
		}
		if got := reportAbuse(t, harness, leased.readToken, ip4, report); got != "202 accepted" {
			t.Fatalf("abuse report: %s", got)
		}

		everyone := []*testMessage{expired, opened, leased, file, burned, cancelled, stuck, filler, pow, removed}
		for _, message := range everyone {
			needles.secret("id of "+message.id.String(), message.id[:])
			needles.text("id text of "+message.id.String(), message.id.String())
			needles.text("hex id text of "+message.id.String(), hex.EncodeToString([]byte(message.id.String())))
			needles.secret("envelope header of "+message.id.String(), message.object[:envelope.HeaderLen])
			if record, err := harness.record(message); err == nil {
				needles.secret("record key of "+message.id.String(), record.Key[:])
			}
		}
		needles.text("contact", contact)
		needles.text("reason", "spam, see")
		if logs := harness.logs.String(); logs != startupLogs {
			t.Fatalf("a line was written before the hourly flush: %s", strings.TrimPrefix(logs, startupLogs))
		}
		harness.server.flushCounts(t.Context())
		if !strings.Contains(harness.logs.String(), "remove: "+syscall.EROFS.Error()) {
			t.Fatal("the failing delete was not logged")
		}
		stolen := harness.stolenCopy()
		reportFile := filepath.Join("abuse", time.Now().UTC().Format(time.DateOnly)+abuse.FileExtension)
		for _, want := range []string{"nylzo.db", "nylzo.db-wal", reportFile} {
			if _, ok := stolen[want]; !ok {
				t.Fatalf("the stolen copy misses %s: %v", want, slices.Sorted(maps.Keys(stolen)))
			}
		}
		reports, err := abuse.Open(testReportKey, stolen[reportFile])
		if err != nil || len(reports) != 1 || reports[0].ID != leased.id.String() || reports[0].Contact != contact ||
			!reports[0].GoodFaith {
			t.Errorf("the operator's key must open the sealed report: %+v %v", reports, err)
		}
		for where, data := range stolen {
			for what, needle := range needles.named {
				if bytes.Contains(data, needle) {
					t.Errorf("%s reveals %s", where, what)
				}
			}
		}
		checkStolenDatabase(t, harness, len(everyone))
	})
}

func (h *testHarness) stolenCopy() map[string][]byte {
	h.t.Helper()
	stolen := map[string][]byte{"log": []byte(h.logs.String())}
	if err := filepath.WalkDir(h.directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(h.directory, path)
		stolen[rel] = data
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
	return stolen
}

// checkStolenDatabase covers D101: a row shows its locator, sealed bytes and two rounded cleanup times, nothing else.
func checkStolenDatabase(t *testing.T, harness *testHarness, created int) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.Join(harness.directory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	columns := column[string](t, database, `SELECT name FROM pragma_table_info('messages') ORDER BY cid`)
	if !slices.Equal(
		columns,
		[]string{"locator", "read_wrap", "manage_wrap", "record", "blob_ref", "blob_wrap", "blob_purge_at", "purge_at"},
	) {
		t.Fatalf("messages keeps more columns than the spec lists: %v", columns)
	}
	if tables := column[string](
		t,
		database,
		`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`,
	); !slices.Equal(
		tables,
		[]string{"messages", "rate_counters", "wal_scrub"},
	) {
		t.Fatalf("the database keeps more tables than the spec lists: %v", tables)
	}
	for query, step := range map[string]time.Duration{
		`SELECT blob_purge_at FROM messages WHERE blob_purge_at IS NOT NULL`: store.BlobPurgeStep,
		`SELECT purge_at FROM messages`:                                      store.RowPurgeStep,
		`SELECT window_start FROM rate_counters`:                             time.Minute,
		`SELECT purge_at FROM rate_counters`:                                 time.Minute,
	} {
		for _, value := range column[int64](t, database, query) {
			if value%step.Milliseconds() != 0 {
				t.Errorf("%s holds %d, which is more precise than %s", query, value, step)
			}
		}
	}
	count := func(query string) int64 { return column[int64](t, database, query)[0] }
	rows, ended := count(
		`SELECT COUNT(*) FROM messages`,
	), count(
		`SELECT COUNT(*) FROM messages WHERE blob_wrap IS NULL`,
	)
	spent := count(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'challenge:%' AND window_start = 0`)
	nonces := count(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'pow:%' AND window_start = 0`)
	if rows != int64(created)+1 || ended != 6 || spent == 0 || nonces != 1 {
		t.Fatalf(
			"the flows did not leave the expected rows: %d rows, %d without a blob key, %d spent challenges, %d PoW nonces",
			rows,
			ended,
			spent,
			nonces,
		)
	}
	if filled := count(
		`SELECT COUNT(*) FROM wal_scrub WHERE generation NOT IN (0, 1) OR filler != zeroblob(length(filler))`,
	); filled != 0 {
		t.Fatalf("wal_scrub holds %d rows with something other than zeros", filled)
	}
	for _, key := range column[string](t, database, `SELECT key FROM rate_counters`) {
		if !slices.ContainsFunc(
			[]string{"global:", "pow:", "challenge:", "sweep:reconcile:"},
			func(prefix string) bool { return strings.HasPrefix(key, prefix) },
		) {
			t.Errorf("counter %q is not one of the address-free keys", key)
		}
	}
}

func column[T any](t *testing.T, database *sql.DB, query string) []T {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []T
	for rows.Next() {
		var value T
		if scanErr := rows.Scan(&value); scanErr != nil {
			t.Fatal(scanErr)
		}
		values = append(values, value)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return values
}

func TestAK11LogsContainNoSecrets(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFScrypt, "log fráze", 1, 3600, "logged text")
		message.address = "203.0.113.55"
		harness.mustCreate(message)
		response, _ := harness.metadata(message)
		codes := []int{response.StatusCode}
		response, _ = harness.claim(message, message.verifier, "")
		lease := response.Header.Get("X-Nylzo-Lease")
		if lease == "" {
			t.Fatalf("the claim gave no lease: %d", response.StatusCode)
		}
		codes = append(codes, response.StatusCode)
		response, _ = harness.blob(message, lease)
		codes = append(codes, response.StatusCode, harness.ack(message, lease).StatusCode)
		response, _ = harness.status(message)
		codes = append(codes, response.StatusCode)
		response, _ = harness.do(
			&testRequest{
				method: http.MethodPost,
				path:   "/api/v1/messages/" + message.id.String() + "/cancel",
				token:  message.manageToken,
			},
		)
		codes = append(codes, response.StatusCode)
		response, _ = harness.do(&testRequest{method: http.MethodGet, path: "/m/" + message.id.String()})
		codes = append(codes, response.StatusCode)
		if want := []int{200, 200, 200, 204, 200, 410, 200}; !slices.Equal(codes, want) {
			t.Fatalf("the flow behind the log check answered %v, want %v", codes, want)
		}
		harness.server.flushCounts(t.Context())
		logs := harness.logs.String()
		if !strings.Contains(logs, `"route":"message"`) {
			t.Fatal("log should count the message routes")
		}
		if strings.Contains(logs, message.id.String()) || strings.Contains(logs, hex.EncodeToString(message.id[:])) {
			t.Fatal("a log line must not name the message, so a stolen log cannot be joined with the database")
		}
		leaseBytes, _ := core.DecodeExact(lease, 32)
		forbidden := map[string][]byte{
			"S": message.secret[:],
			"R": message.link.R[:],
			"V": message.verifierKey[:],
			"M": message.manage[:],
			"K": message.content.K[:],
			"L": leaseBytes,
		}
		for name, forbiddenBytes := range forbidden {
			if strings.Contains(logs, core.Encode(forbiddenBytes)) ||
				strings.Contains(logs, hex.EncodeToString(forbiddenBytes)) {
				t.Fatalf("log contains %s", name)
			}
		}
		for _, needle := range []string{"Authorization", "Bearer", "203.0.113.55", "127.0.0.1", "log fráze", "logged text", "X-Nylzo"} {
			if strings.Contains(logs, needle) {
				t.Fatalf("log contains %q", needle)
			}
		}
	})
}
