package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

type cancelAfterCommit struct{ store.Store }

func (s cancelAfterCommit) RateReserve(ctx context.Context, hit store.RateHit) (int64, bool, error) {
	n, ok, err := s.Store.RateReserve(context.WithoutCancel(ctx), hit)
	if ctx.Err() != nil {
		return 0, false, ctx.Err()
	}
	return n, ok, err
}

func TestReserveKeepsItsSlotWhenTheRequestIsCanceled(t *testing.T) {
	t.Parallel()
	st, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{cfg: config.Server{Rates: config.Rates{GlobalDailyCreates: 3}}, store: cancelAfterCommit{st}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if slot, ok, err := s.reserveGlobalCreate(ctx, time.Unix(1_800_000_000, 0)); err != nil || !ok || slot != 1 {
		t.Fatalf("a canceled request must still learn about the slot it reserved: %d %v %v", slot, ok, err)
	}
}

func TestAbuseReportKeepsItsSlotWhenTheRequestIsCanceled(t *testing.T) {
	t.Parallel()
	st, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{
		store:    cancelAfterCommit{st},
		abuseDir: filepath.Join(t.TempDir(), "abuse"),
		log:      slog.New(slog.DiscardHandler),
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.recordAbuse(ctx, time.Unix(1_800_000_000, 0), core.NewID().String(), "spam"); err != nil {
		t.Fatalf("a report whose slot was reserved must be stored even if the client left: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(s.abuseDir, "2027-01-15.jsonl")); err != nil || len(data) == 0 {
		t.Fatalf("report not stored: %v", err)
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
	t.Parallel()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	st, err := sqlite.Open(t.Context(), filepath.Join(cfg.DataDir, "nylzo.db"))
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
	if c, _ := st.RateGet(
		t.Context(),
		challengeSpentKey+fmt.Sprintf("%x", c[4:challengeSigned]),
		time.Unix(0, 0),
	); c.Count != 0 {
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
	t.Parallel()
	synctest.Test(t, testConcurrentSpendsShareATransaction)
}

func testConcurrentSpendsShareATransaction(t *testing.T) {
	st, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	gated := &gatedSpends{Store: st, gate: make(chan struct{})}
	b := newSpendBatcher(gated)
	spend := func(key string) store.RateHit {
		return store.RateHit{
			Key:         challengeSpentKey + key,
			WindowStart: time.Unix(0, 0),
			PurgeAt:     time.Unix(4102444800, 0),
			Limit:       1,
		}
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
	synctest.Wait()
	if calls := gated.calls(); len(calls) != 1 {
		t.Fatalf("the first spend did not start writing: %v", calls)
	}
	const pairs = 300
	for i := range 2 * pairs {
		reserve(fmt.Sprint(i % pairs))
	}
	synctest.Wait()
	close(gated.gate)
	wg.Wait()
	if granted.Load() != pairs+1 {
		t.Fatalf("%d spends succeeded, want one per distinct nonce (%d)", granted.Load(), pairs+1)
	}
	if calls := gated.calls(); !slices.Equal(calls, []int{1, spendBatchMax, spendBatchMax, 2*pairs - 2*spendBatchMax}) {
		t.Fatalf("spends written as transactions of %v", calls)
	}
}

func TestIDTokenFromTheFutureIsRefused(t *testing.T) {
	t.Parallel()
	s, _ := testServer(t, openStore(t), nil)
	now := time.Now()
	id := core.NewID()
	if !s.validIDToken(id, s.idToken(id, now.Add(5*time.Second)), now) {
		t.Fatal("a token from an instance 5 s ahead must pass")
	}
	if s.validIDToken(id, s.idToken(id, now.Add(time.Minute)), now) {
		t.Fatal("a token that expires more than 605 s from now was accepted")
	}
}
