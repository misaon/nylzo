package storetest

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) rateReserveIsAtomic() {
	ctx := f.t.Context()
	day := baseTime.Truncate(24 * time.Hour)
	hit := store.RateHit{Key: "global:d", WindowStart: day, PurgeAt: day.Add(48 * time.Hour), Limit: 10}
	var granted atomic.Int64
	var wait sync.WaitGroup
	for range 40 {
		wait.Go(func() {
			count, err := f.store.RateReserve(ctx, hit)
			f.errorIf(err != nil, "%v", err)
			if count > 0 {
				granted.Add(1)
			}
		})
	}
	wait.Wait()
	f.fatalIf(granted.Load() != 10, "concurrent reservations: %d granted, want exactly 10", granted.Load())
	f.must(f.store.RateRelease(ctx, hit.Key, hit.WindowStart))
	reserved, err := f.store.RateReserve(ctx, hit)
	f.fatalIf(err != nil || reserved != 10, "a released slot must be reusable: %d %v", reserved, err)
	count, _ := f.store.RateGet(ctx, hit.Key, hit.WindowStart)
	f.fatalIf(count.Count != 10, "count: %+v", count)
}

func (f *fixture) rateHitIsAtomic() {
	ctx := f.t.Context()
	window := baseTime.Truncate(time.Hour)
	hourly := store.RateHit{Key: "hourly:abc", WindowStart: window, PurgeAt: window.Add(2 * time.Hour)}
	daily := store.RateHit{Key: "daily:abc", WindowStart: window, PurgeAt: window.Add(48 * time.Hour)}
	var mutex sync.Mutex
	seen := map[int64]bool{}
	var wait sync.WaitGroup
	for range 100 {
		wait.Go(func() {
			counts, err := f.store.RateHit(ctx, []store.RateHit{hourly, daily})
			f.errorIf(err != nil, "%v", err)
			mutex.Lock()
			defer mutex.Unlock()
			for _, count := range counts[:min(len(counts), 1)] {
				seen[count.Count] = true
			}
		})
	}
	wait.Wait()
	f.fatalIf(len(seen) != 100, "the counter lost increments: %d distinct values", len(seen))
	count, err := f.store.RateGet(ctx, hourly.Key, window)
	f.fatalIf(err != nil || count.Count != 100, "RateGet: %+v %v", count, err)
	missing, err := f.store.RateGet(ctx, "nope", window)
	f.fatalIf(err != nil || missing.Count != 0, "RateGet of a missing counter: %+v %v", missing, err)
	next := hourly
	next.WindowStart, next.PurgeAt = window.Add(time.Hour), window.Add(3*time.Hour)
	counts, err := f.store.RateHit(ctx, []store.RateHit{next})
	f.fatalIf(err != nil || counts[0].Count != 1, "a new window must start at 1: %+v %v", counts, err)
	report, err := f.sweep(f.blobs, window.Add(2*time.Hour))
	f.fatalIf(err != nil || report.Counters != 1, "counter purge: %+v %v", report, err)
}

func (f *fixture) createCountsWithTheRow() {
	ctx := f.t.Context()
	day := baseTime.Truncate(24 * time.Hour)
	hit := store.RateHit{Key: "global:stored:d", WindowStart: day, PurgeAt: day.Add(48 * time.Hour)}
	created := f.create(1, oneHour, false)
	duplicate := created.Row
	err := f.store.Create(ctx, &duplicate, hit)
	f.fatalIf(!errors.Is(err, store.ErrConflict), "duplicate create: %v", err)
	f.createRowWith(hit)
	count, err := f.store.RateGet(ctx, hit.Key, day)
	f.fatalIf(err != nil || count.Count != 1, "only the stored create counts: %+v %v", count, err)
}

func (f *fixture) createRowWith(hit store.RateHit) {
	f.t.Helper()
	fresh := core.NewID()
	record := store.NewRecord(recordsKey, seal.LocatorOf(fresh[:]), newMessage(1, oneHour, false),
		core.NewSecret()[:], core.NewSecret()[:], fresh.String(), new(seal.NewBlobKey()))
	f.must(f.store.Create(f.t.Context(), &record.Row, hit))
}
