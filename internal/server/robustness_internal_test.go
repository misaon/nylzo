package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/server/config"

	"filippo.io/edwards25519"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

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

func testServer(t testing.TB, st store.Store, change func(*config.Server)) (*Server, *lockedBuffer) {
	t.Helper()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	if change != nil {
		change(&cfg)
	}
	logs := &lockedBuffer{}
	s, err := New(cfg, st, nil, WithLogger(slog.New(slog.NewJSONHandler(logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	return s, logs
}

func openStore(t testing.TB) *sqlite.Store {
	t.Helper()
	st, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

type panicOnce struct {
	store.Store
	mu       sync.Mutex
	panicked bool
}

func (p *panicOnce) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	p.mu.Lock()
	first := !p.panicked
	p.panicked = true
	p.mu.Unlock()
	if first {
		panic("test: the store driver panicked")
	}
	return p.Store.RateReserveBatch(ctx, hits)
}

func spendHit(key string) store.RateHit {
	return store.RateHit{
		Key:         challengeSpentKey + key,
		WindowStart: time.Unix(0, 0),
		PurgeAt:     time.Unix(4102444800, 0),
		Limit:       1,
	}
}

func TestASpendPanicFailsOnlyItsBatch(t *testing.T) {
	t.Parallel()
	b := newSpendBatcher(&panicOnce{Store: openStore(t)})
	if _, err := b.reserve(t.Context(), spendHit("a")); !errors.Is(err, errSpendAborted) {
		t.Fatalf("a batch whose write panicked: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := b.reserve(t.Context(), spendHit("b"))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the next batch failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a panic in one batch blocked every later spend")
	}
}

func TestSpendWaitersStopWhenTheirClientLeaves(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		gated := &gatedSpends{Store: openStore(t), gate: make(chan struct{})}
		b := newSpendBatcher(gated)
		go b.reserve(t.Context(), spendHit("writing"))
		synctest.Wait()
		go b.reserve(t.Context(), spendHit("next"))
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)
		go func() {
			_, err := b.reserve(ctx, spendHit("follower"))
			result <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("a waiter whose client left: %v", err)
			}
		default:
			t.Fatal("a waiter whose client left kept waiting for a stalled write")
		}
		close(gated.gate)
		synctest.Wait()
		if c, err := gated.RateGet(
			context.Background(),
			challengeSpentKey+"follower",
			time.Unix(0, 0),
		); err != nil ||
			c.Count != 1 {
			t.Fatalf("the spend of a client that left was not written: %+v %v", c, err)
		}
	})
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
	for _, req := range []*http.Request{
		httptest.NewRequest("POST", "/api/v1/messages/"+id+"/claim", nil),
		httptest.NewRequest("GET", "/api/v1/messages/"+id+"/blob", nil),
	} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "5" {
			t.Fatalf("%s with every stream slot busy: %d %q", req.URL.Path, w.Code, w.Header().Get("Retry-After"))
		}
	}
	s.streams.Release(source, ranges)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/messages/"+id+"/claim", nil))
	if w.Code != http.StatusNotFound || !s.streams.Take(source, ranges) {
		t.Fatalf("a claim with a free slot: %d, or it kept its slot", w.Code)
	}
}

func TestOneSourceCannotHoldEveryStreamSlot(t *testing.T) {
	t.Parallel()
	s, _ := testServer(t, openStore(t), func(c *config.Server) { c.MaxStreams = 64 })
	claim := func(addr string) int {
		req := httptest.NewRequest("POST", "/api/v1/messages/"+core.NewID().String()+"/claim", nil)
		req.RemoteAddr = addr + ":1234"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
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
	if _, err := conn.Write(out); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	head := make([]byte, 9)
	for {
		if _, err := io.ReadFull(r, head); err != nil {
			t.Fatalf("the server kept a connection that sent an oversized frame: %v", err)
		}
		size := int(head[0])<<16 | int(head[1])<<8 | int(head[2])
		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			t.Fatal(err)
		}
		if head[3] == 0x7 {
			if code := binary.BigEndian.Uint32(payload[4:8]); code != 0x6 {
				t.Fatalf("GOAWAY with error %#x, want FRAME_SIZE_ERROR", code)
			}
			return
		}
	}
}

func TestSweepLogAttrsAcceptAnEmptyJoin(t *testing.T) {
	t.Parallel()
	attrs := SweepLogAttrs(store.SweepReport{}, fmt.Errorf("%w %w", nil, nil))
	if len(attrs) < 4 || attrs[len(attrs)-4] != "errors" || attrs[len(attrs)-3] != 1 {
		t.Fatalf("attrs: %v", attrs)
	}
}

func TestRunSweeperStopsWithoutAnError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, logs := testServer(t, openStore(t), func(c *config.Server) { c.SweepInterval = time.Minute })
		blobs, err := disk.New(s.cfg.DataDir)
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
	st := openStore(t)
	for name, change := range map[string]func(*config.Server){
		"no PoW slot": func(c *config.Server) { c.PoWConcurrency = 0 },
	} {
		cfg := config.Defaults()
		cfg.Pepper = bytes.Repeat([]byte{1}, 32)
		change(&cfg)
		if _, err := New(cfg, st, nil); !errors.Is(err, config.ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestAuthorizeDoesTheSameWorkForKeysThatFailTheKeyCheck(t *testing.T) {
	s, _ := testServer(t, openStore(t), nil)
	path := "/api/v1/messages/" + core.NewID().String()
	var offCurve [core.PublicKeyLen]byte
	for i := byte(2); ; i++ {
		offCurve[0] = i
		if _, err := new(edwards25519.Point).SetBytes(offCurve[:]); err != nil {
			break
		}
	}
	request := func(pk *[core.PublicKeyLen]byte) *http.Request {
		c, _ := s.issueChallenge(s.clock())
		sig := core.SignRequest(core.NewSecret(), core.PurposeRead, "GET", path, c, nil)
		if pk != nil {
			sig.PublicKey = *pk
		}
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", sig.Authorization())
		return r.WithContext(context.WithValue(r.Context(), spentKey{}, &sig.Challenge))
	}
	const samples = 300
	var wrong, off []time.Duration
	for range samples {
		for _, pk := range []*[core.PublicKeyLen]byte{nil, &offCurve} {
			r := request(pk)
			start := time.Now()
			if _, ok, err := s.authorize(
				r.Context(),
				r,
				path[len("/api/v1/messages/"):],
				pepper.Read,
				nil,
			); ok ||
				err != nil {
				t.Fatalf("authorize: %v %v", ok, err)
			}
			if pk == nil {
				wrong = append(wrong, time.Since(start))
			} else {
				off = append(off, time.Since(start))
			}
		}
	}
	median := func(d []time.Duration) time.Duration {
		slices.Sort(d)
		return d[len(d)/2]
	}
	// gstack-timing-ok: uniform work for every failed key is the contract under test (spec §5.5)
	if mw, mo := median(wrong), median(off); mo*130 < mw*100 {
		t.Fatalf("a key that fails the key check is answered in %s, a wrong key in %s", mo, mw)
	}
}
