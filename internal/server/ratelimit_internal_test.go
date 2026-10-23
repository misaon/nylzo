package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestSourceLimiterEvictsIdleSourcesFirst(t *testing.T) {
	l := newKeyedLimiter(1, 1)
	l.max = 16
	now := time.Unix(1_800_000_000, 0)
	for i := range 3 {
		l.allow(bucket{1, byte(i)}, now)
	}
	later := now.Add(2 * time.Second)
	for i := range 13 {
		key := bucket{2, byte(i)}
		if !l.allow(key, later) || l.allow(key, later) {
			t.Fatal("throttled source burst")
		}
	}
	if !l.allow(bucket{3}, later) {
		t.Fatal("a new source was refused at the cap")
	}
	for i := range 13 {
		if l.allow(bucket{2, byte(i)}, later) {
			t.Fatalf("eviction reset throttled source %d while idle sources were available", i)
		}
	}
}

func TestSourceLimiterKeepsMostSourcesWhenFull(t *testing.T) {
	l := newKeyedLimiter(1, 1)
	l.max = 64
	now := time.Unix(1_800_000_000, 0)
	for i := range 64 {
		l.allow(bucket{byte(i)}, now)
	}
	l.allow(bucket{0, 1}, now)
	throttled := 0
	for i := range 64 {
		if _, ok := l.buckets[bucket{byte(i)}]; ok {
			throttled++
		}
	}
	if throttled < 48 || throttled == 64 {
		t.Fatalf("a full table must drop a small share of sources, kept %d of 64", throttled)
	}
}

type cancelAfterCommit struct{ store.Store }

func (s cancelAfterCommit) RateReserve(ctx context.Context, hit store.RateHit) (int64, bool, error) {
	n, ok, err := s.Store.RateReserve(context.WithoutCancel(ctx), hit)
	if ctx.Err() != nil {
		return 0, false, ctx.Err()
	}
	return n, ok, err
}

func TestReserveKeepsItsSlotWhenTheRequestIsCanceled(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{cfg: Config{Rates: Rates{GlobalDailyCreates: 3}}, store: cancelAfterCommit{st}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if slot, ok, err := s.reserveGlobalCreate(ctx, time.Unix(1_800_000_000, 0)); err != nil || !ok || slot != 1 {
		t.Fatalf("a canceled request must still learn about the slot it reserved: %d %v %v", slot, ok, err)
	}
}

func TestAbuseReportKeepsItsSlotWhenTheRequestIsCanceled(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: cancelAfterCommit{st}, abusePath: filepath.Join(t.TempDir(), "abuse-reports.jsonl"), log: slog.New(slog.DiscardHandler)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.recordAbuse(ctx, time.Unix(1_800_000_000, 0), core.NewID().String(), "spam"); err != nil {
		t.Fatalf("a report whose slot was reserved must be stored even if the client left: %v", err)
	}
	if data, err := os.ReadFile(s.abusePath); err != nil || len(data) == 0 {
		t.Fatalf("report not stored: %v", err)
	}
}

func TestAddrCountersRefuseWithAtLeastOneSecond(t *testing.T) {
	c := newAddrCounters()
	w := hourWindow(time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC))
	last := w.end.Add(-time.Nanosecond)
	hit := []addrHit{{kind: "create:h", window: w, limit: 1}}
	if retry := c.hit(hit, w.start); retry != 0 {
		t.Fatalf("first request refused: %v", retry)
	}
	if retry := c.hit(hit, last); retry != time.Second {
		t.Fatalf("a request over the limit just before the window ends got Retry-After %v", retry)
	}
	if retry := c.hit([]addrHit{{kind: "create:h", window: hourWindow(w.end), limit: 1}}, w.end); retry != 0 {
		t.Fatalf("the next window still counts the old requests: %v", retry)
	}
}

func TestAddrCountersStopAtTheFirstRefusal(t *testing.T) {
	c := newAddrCounters()
	w := hourWindow(time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC))
	fine, coarse := bucket{1}, bucket{2}
	hits := []addrHit{{kind: "k", bucket: fine, window: w, limit: 1}, {kind: "k:c", bucket: coarse, window: w, limit: 10}}
	for range 3 {
		c.hit(hits, w.start)
	}
	if got := c.tables["k:c"][coarse].count; got != 1 {
		t.Fatalf("refused requests were counted against the coarse bucket: %d", got)
	}
}

func TestAddrCountersStayBounded(t *testing.T) {
	c := newAddrCounters()
	c.max = 64
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	old, cur := hourWindow(now.Add(-time.Hour)), hourWindow(now)
	for i := range c.max {
		c.hit([]addrHit{{kind: "k", bucket: bucket{byte(i)}, window: old}}, old.start)
	}
	c.hit([]addrHit{{kind: "k", bucket: bucket{0xff, 0xff}, window: cur}}, now)
	if n := len(c.tables["k"]); n != 1 {
		t.Fatalf("expired buckets were kept: %d", n)
	}
	for i := range 4 * c.max {
		c.hit([]addrHit{{kind: "k", bucket: bucket{byte(i), byte(i >> 8), 1}, window: cur}}, now)
	}
	if n := len(c.tables["k"]); n > c.max {
		t.Fatalf("the table grew past its bound: %d", n)
	}
}

func TestAddressBucketsComeFromAProcessKey(t *testing.T) {
	a, b := newAddrKey(), newAddrKey()
	addr := netip.MustParseAddr("203.0.113.77")
	if a.bucket(addr, false) == b.bucket(addr, false) && a.bucket(addr, true) == b.bucket(addr, true) {
		t.Fatal("two processes derived the same buckets, so the key is not random per process")
	}
	k := newAddrKey()
	same := netip.MustParseAddr("203.0.113.78")
	mapped := netip.MustParseAddr("::ffff:203.0.113.77")
	if k.bucket(addr, false) == k.bucket(same, false) || k.bucket(addr, true) != k.bucket(same, true) || k.bucket(addr, false) != k.bucket(mapped, false) {
		t.Fatal("IPv4 buckets do not group by /32 and /24")
	}
	v1, v2, v3, v4 := netip.MustParseAddr("2001:db8:1:2:3:4:5:6"), netip.MustParseAddr("2001:db8:1:2:ffff:ffff:ffff:ffff"), netip.MustParseAddr("2001:db8:1:3::1"), netip.MustParseAddr("2001:db8:2::1")
	if k.bucket(v1, false) != k.bucket(v2, false) || k.bucket(v1, false) == k.bucket(v3, false) || k.bucket(v1, true) != k.bucket(v3, true) || k.bucket(v1, true) == k.bucket(v4, true) {
		t.Fatal("IPv6 buckets do not group by /64 and /48")
	}
}

type failingSpends struct {
	store.Store
	fail bool
}

func (s *failingSpends) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	if s.fail {
		return nil, errors.New("test: the store is down")
	}
	return s.Store.RateReserveBatch(ctx, hits)
}

func TestAFailedSpendRefusesTheChallenge(t *testing.T) {
	cfg := Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	st, err := sqlite.Open(filepath.Join(cfg.DataDir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	failing := &failingSpends{Store: st, fail: true}
	s, err := New(cfg, failing, nil, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/messages/" + core.NewID().String()
	c, _ := s.issueChallenge(s.clock())
	auth := core.SignRequest(core.NewSecret(), core.PurposeRead, "GET", path, c, nil).Authorization()
	send := func() int {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := send(); code != http.StatusInternalServerError {
		t.Fatalf("a spend the store could not record: %d", code)
	}
	failing.fail = false
	if code := send(); code != http.StatusNotFound {
		t.Fatalf("the same process accepted the challenge after a failed spend: %d", code)
	}
	if c, _ := st.RateGet(t.Context(), challengeSpentKey+fmt.Sprintf("%x", c[4:challengeSigned]), time.Unix(0, 0)); c.Count != 0 {
		t.Fatal("the repeat reached the store although the process had seen the nonce")
	}
}

type gatedSpends struct {
	store.Store
	gate    chan struct{}
	mu      sync.Mutex
	batches []int
}

func (s *gatedSpends) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	s.mu.Lock()
	first := len(s.batches) == 0
	s.batches = append(s.batches, len(hits))
	s.mu.Unlock()
	if first {
		<-s.gate
	}
	return s.Store.RateReserveBatch(ctx, hits)
}

func (s *gatedSpends) calls() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.batches)
}

func TestConcurrentSpendsShareATransaction(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	gated := &gatedSpends{Store: st, gate: make(chan struct{})}
	b := newSpendBatcher(gated)
	spend := func(key string) store.RateHit {
		return store.RateHit{Key: challengeSpentKey + key, WindowStart: time.Unix(0, 0), PurgeAt: time.Unix(4102444800, 0), Limit: 1}
	}
	var wg sync.WaitGroup
	var granted atomic.Int64
	reserve := func(key string) {
		wg.Go(func() {
			ok, err := b.reserve(t.Context(), spend(key))
			if err != nil {
				t.Error(err)
			}
			if ok {
				granted.Add(1)
			}
		})
	}
	reserve("first")
	waitFor(t, func() bool { return len(gated.calls()) == 1 })
	const pairs = 300
	for i := range 2 * pairs {
		reserve(fmt.Sprint(i % pairs))
	}
	waitFor(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		queued := 0
		for _, batch := range b.queue {
			queued += len(batch.hits)
		}
		return queued == 2*pairs
	})
	close(gated.gate)
	wg.Wait()
	if granted.Load() != pairs+1 {
		t.Fatalf("%d spends succeeded, want one per distinct nonce (%d)", granted.Load(), pairs+1)
	}
	if calls := gated.calls(); !slices.Equal(calls, []int{1, spendBatchMax, spendBatchMax, 2*pairs - 2*spendBatchMax}) {
		t.Fatalf("spends written as transactions of %v", calls)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
