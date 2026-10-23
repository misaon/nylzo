package server_test

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store"
)

func TestGlobalCounterCountsOnlyAdmittedCreates(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 1000
	h := newHarness(t, cfg)
	for range 25 {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = "203.0.113.5"
		h.create(m)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, err := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); err != nil || c.Count != 20 {
		t.Fatalf("rate-limited attempts moved the global counter: %+v %v", c, err)
	}
	small := baseConfig(pepperA, nil)
	small.Rates.GlobalDailyCreates = 40
	h.reopen(small)
	for i := range 3 {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("198.51.100.%d", i+1)
		if resp, _ := h.create(m); resp.StatusCode != 403 {
			t.Fatalf("create without pow at 50 percent: %d", resp.StatusCode)
		}
	}
	if c, _ := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); c.Count != 20 {
		t.Fatalf("PoW failures moved the global counter: %+v", c)
	}
	h.reopen(cfg)
	existing := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	existing.ip = "198.51.100.10"
	h.mustCreate(existing)
	if resp, _ := h.create(existing); resp.StatusCode != 409 {
		t.Fatalf("duplicate id: %d", resp.StatusCode)
	}
	wrongKDF := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	wrongKDF.ip = "198.51.100.11"
	headerKDF2 := mutateHeader(t, wrongKDF, func(f map[string]any) { f["kdf_id"] = 2; f["verifier_key"] = publicKey(core.NewSecret()) })
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/messages", body: wrongKDF.object, ip: wrongKDF.ip, headers: map[string]string{"X-Nylzo-Create": headerKDF2}}); resp.StatusCode != 400 {
		t.Fatalf("body kdf differs from the header: %d", resp.StatusCode)
	}
	if c, _ := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); c.Count != 21 {
		t.Fatalf("only stored messages may move the global counter: %+v", c)
	}
}

type slotSignals struct {
	store.Store
	reserved chan struct{}
	released chan struct{}
}

func (s *slotSignals) RateReserve(ctx context.Context, hit store.RateHit) (int64, bool, error) {
	n, ok, err := s.Store.RateReserve(ctx, hit)
	if hit.Key == server.GlobalCreatesKey {
		s.reserved <- struct{}{}
	}
	return n, ok, err
}

func (s *slotSignals) RateRelease(ctx context.Context, key string, windowStart time.Time) error {
	err := s.Store.RateRelease(ctx, key, windowStart)
	if key == server.GlobalCreatesKey {
		s.released <- struct{}{}
	}
	return err
}

func TestDisconnectedCreateReturnsItsCapSlot(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 10
	h := newHarness(t, cfg)
	signals := &slotSignals{Store: h.store, reserved: make(chan struct{}, 1), released: make(chan struct{}, 1)}
	h.store = signals
	h.reopen(h.cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	conn, err := net.Dial("tcp", h.listen())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(conn, "POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n", len(m.object), m.createHeader(0))
	conn.Write(m.object[:100])
	wait := func(ch chan struct{}, what string) {
		select {
		case <-ch:
		// gstack-timing-ok: an upper bound on waiting for the event, not a measurement
		case <-time.After(5 * time.Second):
			t.Fatalf("the create never %s its cap slot", what)
		}
	}
	wait(signals.reserved, "reserved")
	conn.Close()
	wait(signals.released, "released")
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, err := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); err != nil || c.Count != 0 {
		t.Fatalf("a disconnected create kept its cap slot: %+v %v", c, err)
	}
}

func TestCapFullOfPendingCreatesAsksForAShortRetry(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 2
	h := newHarness(t, cfg)
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	hit := store.RateHit{Key: server.GlobalCreatesKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour), Limit: 2}
	for range 2 {
		if _, ok, err := h.store.RateReserve(t.Context(), hit); err != nil || !ok {
			t.Fatal(err)
		}
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	resp, _ := h.create(m)
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") != "60" {
		t.Fatalf("pending creates must ask for a short retry: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if _, err := h.store.RateHit(t.Context(), []store.RateHit{{Key: server.GlobalStoredKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour)}, {Key: server.GlobalStoredKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	resp, _ = h.create(m)
	if retry, _ := strconv.Atoi(resp.Header.Get("Retry-After")); resp.StatusCode != 503 || retry < 3600 {
		t.Fatalf("a cap full of stored messages must point to the next day: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

type staleGateStore struct{ store.Store }

func (s staleGateStore) RateGet(ctx context.Context, key string, ws time.Time) (store.RateCount, error) {
	if key == server.GlobalCreatesKey {
		return store.RateCount{}, nil
	}
	return s.Store.RateGet(ctx, key, ws)
}

func TestSlotPastHalfTheCapNeedsPoW(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 4
	h := newHarness(t, cfg)
	h.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "a"))
	h.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "b"))
	inner := h.store
	h.store = staleGateStore{Store: inner}
	h.reopen(h.cfg)
	if resp, body := h.create(newMessage(t, core.KDFNone, "", 1, 3600, "c")); resp.StatusCode != 403 || decodeJSON(t, body)["error"] != "pow" {
		t.Fatalf("the third of four slots was taken without PoW: %d %s", resp.StatusCode, body)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, err := inner.RateGet(t.Context(), server.GlobalCreatesKey, day); err != nil || c.Count != 2 {
		t.Fatalf("the refused create must return its slot: %+v %v", c, err)
	}
}

func TestStoredCapPointsToTheNextDay(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 1
	h := newHarness(t, cfg)
	h.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "x"))
	resp, _ := h.create(newMessage(t, core.KDFNone, "", 1, 3600, "y"))
	if retry, _ := strconv.Atoi(resp.Header.Get("Retry-After")); resp.StatusCode != 503 || retry < 3600 {
		t.Fatalf("a cap filled by stored messages: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

type fillingStore struct {
	store.Store
	once sync.Once
	fill func()
}

func (s *fillingStore) RateGet(ctx context.Context, key string, ws time.Time) (store.RateCount, error) {
	c, err := s.Store.RateGet(ctx, key, ws)
	if key == server.GlobalCreatesKey {
		s.once.Do(s.fill)
	}
	return c, err
}

func TestReservationLostAfterTheGateAnswers503(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 1
	h := newHarness(t, cfg)
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	inner := h.store
	h.store = &fillingStore{Store: inner, fill: func() {
		if _, ok, err := inner.RateReserve(context.Background(), store.RateHit{Key: server.GlobalCreatesKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour), Limit: 1}); err != nil || !ok {
			t.Error(ok, err)
		}
	}}
	h.reopen(h.cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	if resp, _ := h.create(m); resp.StatusCode != 503 {
		t.Fatalf("a create that lost its reservation: %d", resp.StatusCode)
	}
	if c, _ := inner.RateGet(t.Context(), server.GlobalCreatesKey, day); c.Count != 1 {
		t.Fatalf("the cap was exceeded: %+v", c)
	}
	if resp, _ := h.metadata(m); resp.StatusCode != 404 {
		t.Fatalf("a refused create stored a row: %d", resp.StatusCode)
	}
}
