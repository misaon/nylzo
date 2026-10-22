package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestSourceLimiterEvictsIdleSourcesFirst(t *testing.T) {
	l := newKeyedLimiter(1, 1)
	l.max = 16
	now := time.Unix(1_800_000_000, 0)
	for i := range 3 {
		l.allow([pepper.BucketLen]byte{1, byte(i)}, now)
	}
	later := now.Add(2 * time.Second)
	for i := range 13 {
		key := [pepper.BucketLen]byte{2, byte(i)}
		if !l.allow(key, later) || l.allow(key, later) {
			t.Fatal("throttled source burst")
		}
	}
	if !l.allow([pepper.BucketLen]byte{3}, later) {
		t.Fatal("a new source was refused at the cap")
	}
	for i := range 13 {
		if l.allow([pepper.BucketLen]byte{2, byte(i)}, later) {
			t.Fatalf("eviction reset throttled source %d while idle sources were available", i)
		}
	}
}

func TestSourceLimiterKeepsMostSourcesWhenFull(t *testing.T) {
	l := newKeyedLimiter(1, 1)
	l.max = 64
	now := time.Unix(1_800_000_000, 0)
	for i := range 64 {
		l.allow([pepper.BucketLen]byte{byte(i)}, now)
	}
	l.allow([pepper.BucketLen]byte{0, 1}, now)
	throttled := 0
	for i := range 64 {
		if _, ok := l.buckets[[pepper.BucketLen]byte{byte(i)}]; ok {
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
	if err := s.recordAbuse(ctx, time.Unix(1_800_000_000, 0), core.NewID().String(), [pepper.BucketLen]byte{}, "spam"); err != nil {
		t.Fatalf("a report whose slot was reserved must be stored even if the client left: %v", err)
	}
	if data, err := os.ReadFile(s.abusePath); err != nil || len(data) == 0 {
		t.Fatalf("report not stored: %v", err)
	}
}
