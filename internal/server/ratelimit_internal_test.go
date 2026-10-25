package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
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
	s := &Server{settings: config.Server{Rates: config.Rates{GlobalDailyCreates: 3}}, store: cancelAfterCommit{st}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if slot, err := s.reserveGlobalCreate(
		ctx,
		time.Unix(1_800_000_000, 0),
	); err != nil || !slot.reserved ||
		slot.number != 1 {
		t.Fatalf("a canceled request must still learn about the slot it reserved: %+v %v", slot, err)
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
	s, err := New(&cfg, failing, nil, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/messages/" + core.NewID().String()
	c, _ := s.challenges.Issue(s.clock())
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
}
