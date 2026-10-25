package storetest

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) rateReserveIsAtomic() {
	t, ctx := f.t, f.t.Context()
	day := baseTime.Truncate(24 * time.Hour)
	hit := store.RateHit{Key: "global:d", WindowStart: day, PurgeAt: day.Add(48 * time.Hour), Limit: 10}
	var granted atomic.Int64
	var wait sync.WaitGroup
	for range 40 {
		wait.Go(func() {
			count, err := f.store.RateReserve(ctx, hit)
			if err != nil {
				t.Error(err)
			}
			if count > 0 {
				granted.Add(1)
			}
		})
	}
	wait.Wait()
	if granted.Load() != 10 {
		t.Fatalf("concurrent reservations: %d granted, want exactly 10", granted.Load())
	}
	if err := f.store.RateRelease(ctx, hit.Key, hit.WindowStart); err != nil {
		t.Fatal(err)
	}
	if count, err := f.store.RateReserve(ctx, hit); err != nil || count != 10 {
		t.Fatalf("a released slot must be reusable: %d %v", count, err)
	}
	if count, _ := f.store.RateGet(ctx, hit.Key, hit.WindowStart); count.Count != 10 {
		t.Fatalf("count: %+v", count)
	}
}

func (f *fixture) rateReserveBatch() {
	t, ctx := f.t, f.t.Context()
	spend := func(key string) store.RateHit {
		return store.RateHit{Key: key, WindowStart: time.Unix(0, 0), PurgeAt: baseTime.Add(time.Hour), Limit: 1}
	}
	reserved, err := f.store.RateReserveBatch(ctx, []store.RateHit{spend("a"), spend("b"), spend("a")})
	if err != nil || !slices.Equal(reserved, []bool{true, true, false}) {
		t.Fatalf("first batch: %v %v", reserved, err)
	}
	reserved, err = f.store.RateReserveBatch(ctx, []store.RateHit{spend("c"), spend("b")})
	if err != nil || !slices.Equal(reserved, []bool{true, false}) {
		t.Fatalf("second batch: %v %v", reserved, err)
	}
	if count, _ := f.store.RateGet(ctx, "a", time.Unix(0, 0)); count.Count != 1 {
		t.Fatalf("a refused hit still counted: %+v", count)
	}
	var granted atomic.Int64
	var wait sync.WaitGroup
	for range 20 {
		wait.Go(func() {
			batch, batchErr := f.store.RateReserveBatch(
				ctx,
				[]store.RateHit{spend("shared"), spend(core.NewID().String())},
			)
			if batchErr != nil || !batch[1] {
				t.Errorf("concurrent batch: %v %v", batch, batchErr)
				return
			}
			if batch[0] {
				granted.Add(1)
			}
		})
	}
	wait.Wait()
	if granted.Load() != 1 {
		t.Fatalf("concurrent batches reserved one key %d times", granted.Load())
	}
}

func (f *fixture) rateHitIsAtomic() {
	t, ctx := f.t, f.t.Context()
	window := baseTime.Truncate(time.Hour)
	hourly := store.RateHit{Key: "hourly:abc", WindowStart: window, PurgeAt: window.Add(2 * time.Hour)}
	daily := store.RateHit{Key: "daily:abc", WindowStart: window, PurgeAt: window.Add(48 * time.Hour)}
	var mutex sync.Mutex
	seen := map[int64]bool{}
	var wait sync.WaitGroup
	for range 100 {
		wait.Go(func() {
			counts, err := f.store.RateHit(ctx, []store.RateHit{hourly, daily})
			if err != nil {
				t.Error(err)
				return
			}
			mutex.Lock()
			defer mutex.Unlock()
			seen[counts[0].Count] = true
		})
	}
	wait.Wait()
	if len(seen) != 100 {
		t.Fatalf("the counter lost increments: %d distinct values", len(seen))
	}
	if count, err := f.store.RateGet(ctx, hourly.Key, window); err != nil || count.Count != 100 {
		t.Fatalf("RateGet: %+v %v", count, err)
	}
	if missing, err := f.store.RateGet(ctx, "nope", window); err != nil || missing.Count != 0 {
		t.Fatalf("RateGet of a missing counter: %+v %v", missing, err)
	}
	next := hourly
	next.WindowStart, next.PurgeAt = window.Add(time.Hour), window.Add(3*time.Hour)
	if counts, err := f.store.RateHit(ctx, []store.RateHit{next}); err != nil || counts[0].Count != 1 {
		t.Fatalf("a new window must start at 1: %+v %v", counts, err)
	}
	if report, err := f.sweep(f.blobs, window.Add(2*time.Hour)); err != nil || report.Counters != 1 {
		t.Fatalf("counter purge: %+v %v", report, err)
	}
}

func (f *fixture) createCountsWithTheRow() {
	t, ctx := f.t, f.t.Context()
	day := baseTime.Truncate(24 * time.Hour)
	hit := store.RateHit{Key: "global:stored:d", WindowStart: day, PurgeAt: day.Add(48 * time.Hour)}
	created := f.create(1, oneHour, false)
	duplicate := created.Row
	if err := f.store.Create(ctx, &duplicate, hit); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	f.createRowWith(hit)
	if count, err := f.store.RateGet(ctx, hit.Key, day); err != nil || count.Count != 1 {
		t.Fatalf("only the stored create counts: %+v %v", count, err)
	}
}

func (f *fixture) createRowWith(hit store.RateHit) {
	f.t.Helper()
	fresh := core.NewID()
	record := store.NewRecord(seal.LocatorOf(fresh[:]), newMessage(1, oneHour, false), core.NewSecret()[:],
		core.NewSecret()[:], fresh.String(), seal.NewKey())
	if err := f.store.Create(f.t.Context(), &record.Row, hit); err != nil {
		f.t.Fatal(err)
	}
}
