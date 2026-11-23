package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) expiry() {
	created := f.create(1, oneMinute, false)
	report, err := f.sweep(f.blobs, baseTime.Add(61*time.Second))
	f.fatalIf(err != nil || report.Blobs != 0, "the sweep must wait for blob_purge_at: %+v %v", report, err)
	_, err = f.claim(created, baseTime.Add(61*time.Second), 1)
	f.fatalIf(!endedAs(err, store.StateExpired), "a claim on an expired message the sweep has not reached: %v", err)
	report, err = f.sweep(f.blobs, baseTime.Add(store.BlobPurgeStep))
	f.fatalIf(err != nil || report.Blobs != 1, "sweep: %+v %v", report, err)
	record := f.get(created)
	f.fatalIf(record.State(baseTime.Add(time.Hour)) != store.StateExpired || record.HasBlob() ||
		f.blobExists(created.reference()) || !record.EndedAt(baseTime.Add(time.Hour)).Equal(created.Message.ExpiresAt),
		"expected an expired message without its blob, got %+v", record.Message)
	purgeAt := baseTime.Add(store.TombstoneTTL + time.Hour)
	report, err = f.sweep(f.blobs, purgeAt.Add(-time.Second))
	f.fatalIf(err != nil || report.Rows != 0, "an early row purge: %+v %v", report, err)
	report, err = f.sweep(f.blobs, purgeAt)
	f.fatalIf(err != nil || report.Rows != 1, "row purge: %+v %v", report, err)
	_, err = f.store.Load(f.t.Context(), created.Row.Locator)
	f.fatalIf(!errors.Is(err, store.ErrNotFound), "after the purge: %v", err)
}

func (f *fixture) sweepSurvivesBlobErrors() {
	bad := f.create(1, oneMinute, false)
	good := f.create(1, oneMinute, false)
	window := baseTime.Truncate(time.Hour)
	hit := store.RateHit{Key: "k", WindowStart: window, PurgeAt: window.Add(time.Minute)}
	_, err := f.store.RateHit(f.t.Context(), []store.RateHit{hit})
	f.must(err)
	due := baseTime.Add(store.BlobPurgeStep)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDelete(bad.reference(), ErrInjected)
	report, err := f.sweep(failing, due)
	f.fatalIf(!errors.Is(err, ErrInjected) || report.Blobs != 1 || report.Counters != 1, "%+v %v", report, err)
	record := f.get(bad)
	f.fatalIf(record.HasBlob() || record.Row.BlobReference != bad.reference() || !f.blobExists(bad.reference()),
		"a blob whose delete failed must be unreadable and keep its blob_ref for the next sweep")
	f.fatalIf(f.get(good).Row.BlobReference != "" || f.blobExists(good.reference()),
		"one failing blob blocked the rest of the page")
	report, err = f.sweep(f.blobs, due.Add(time.Minute))
	f.fatalIf(err != nil || report.Blobs != 1 || f.blobExists(bad.reference()),
		"the next sweep must retry: %+v %v", report, err)
}

func (f *fixture) sweepPagesPastFailures() {
	rows := make([]*message, 0, store.SweepBatch+1)
	for range store.SweepBatch + 1 {
		rows = append(rows, f.createRow(1, oneMinute, false))
	}
	slices.SortFunc(rows, func(first, second *message) int {
		return bytes.Compare(first.Row.Locator[:], second.Row.Locator[:])
	})
	failingReferences := make([]string, 0, store.SweepBatch)
	for _, row := range rows[:store.SweepBatch] {
		failingReferences = append(failingReferences, row.reference())
	}
	ctx, cancel := context.WithTimeout(f.t.Context(), time.Minute)
	defer cancel()
	failing := NewFaultyBlobs(f.blobs)
	for _, reference := range failingReferences {
		failing.FailDelete(reference, ErrInjected)
	}
	report, err := store.SweepAll(ctx, f.store, failing, baseTime.Add(store.BlobPurgeStep))
	f.fatalIf(
		!errors.Is(err, ErrInjected) || report.Blobs != 1 || f.get(rows[store.SweepBatch]).Row.BlobReference != "",
		"a full page of failures must not hide the rows behind it: %+v",
		report,
	)
}

func (f *fixture) sweepDeletesAPageTogether() {
	unspent := f.create(2, oneMinute, false)
	spent := f.create(1, oneMinute, false)
	_, err := f.claim(spent, baseTime, 1)
	f.must(err)
	burned := f.create(1, oneMinute, false)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(burned.reference(), ErrInjected)
	_, err = f.end(burned, store.StateDeleted, baseTime, failing)
	f.fatalIf(!errors.Is(err, ErrInjected), "%v", err)
	live := f.create(1, oneHour, false)
	recorder := &batchRecorder{Blobs: f.blobs, store: f.store, rows: map[string]*message{}}
	for _, created := range []*message{unspent, spent, burned} {
		recorder.rows[created.reference()] = created
	}
	report, err := f.sweep(recorder, baseTime.Add(store.BlobPurgeStep))
	f.fatalIf(err != nil || report.Blobs != 3, "sweep: %+v %v", report, err)
	f.fatalIf(
		recorder.batches != 1 || len(recorder.references) != 3 || recorder.deletes != 0 || len(recorder.readable) != 0,
		"a page must lose its blob keys first and then delete its blobs in one batch: %+v",
		recorder,
	)
	f.checkSweptPage(unspent, spent, burned)
	record := f.get(live)
	f.fatalIf(!record.HasBlob() || !f.blobExists(live.reference()), "the sweep deleted a live blob")
}

func (f *fixture) checkSweptPage(unspent, spent, burned *message) {
	later := baseTime.Add(time.Hour)
	for created, want := range map[*message]struct {
		state store.State
		ended time.Time
	}{
		unspent: {store.StateExpired, unspent.Message.ExpiresAt},
		spent:   {store.StateOpened, spent.Message.ExpiresAt},
		burned:  {store.StateDeleted, baseTime},
	} {
		record := f.get(created)
		f.fatalIf(record.State(later) != want.state || !record.EndedAt(later).Equal(want.ended) ||
			f.blobExists(created.reference()),
			"want a %s message ended at %v and no blob: %+v", want.state, want.ended, record.Message)
	}
}

func (f *fixture) sweepPurgesInBatches() {
	hits := make([]store.RateHit, 0, 2500)
	for i := range 2500 {
		hits = append(hits, store.RateHit{Key: fmt.Sprintf("k%d", i), WindowStart: baseTime, PurgeAt: baseTime})
	}
	_, err := f.store.RateHit(f.t.Context(), hits)
	f.must(err)
	report, err := f.sweep(f.blobs, baseTime)
	f.fatalIf(err != nil || report.Counters != 2500, "batched purge: %+v %v", report, err)
}

// reconcile covers that an orphan waits for the blob_purge_at it carries, which hides its TTL until then.
func (f *fixture) reconcile() {
	ctx := f.t.Context()
	fresh, failingA, failingB, orphan := core.NewID().String(), core.NewID().String(), core.NewID().String(),
		core.NewID().String()
	purgeAt := f.putOrphans(fresh, failingA, failingB, orphan)
	live := f.create(1, oneMinute, false)
	report, err := store.Reconcile(ctx, f.store, f.blobs, purgeAt.Add(-time.Second))
	f.fatalIf(err != nil || !report.Listed || report.Orphans != 0 || !f.blobExists(fresh),
		"an orphan must stay until the blob_purge_at it carries: %+v %v", report, err)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDelete(failingA, ErrInjected)
	failing.FailDelete(failingB, ErrInjected)
	report, err = store.Reconcile(ctx, f.store, failing, purgeAt)
	f.fatalIf(!errors.Is(err, ErrInjected) || !report.Listed || report.Orphans != 2 || f.blobExists(fresh) ||
		f.blobExists(orphan), "a failing delete must not stop the others: %+v %v", report, err)
	f.fatalIf(failing.DeleteAttempts(failingA) != 1 || failing.DeleteAttempts(failingB) != 1 ||
		!f.blobExists(live.reference()), "the reconcile skipped an orphan after a failure or deleted a live blob")
}

// putOrphans stores blobs that no row holds and returns the latest blob_purge_at among them.
func (f *fixture) putOrphans(references ...string) time.Time {
	f.t.Helper()
	var purgeAt time.Time
	for _, reference := range references {
		expiresAt := time.Now().Add(time.Hour)
		_, err := f.blobs.PutExpiring(f.t.Context(), reference, strings.NewReader("x"), 1, expiresAt)
		f.must(err)
		if blobPurgeAt := store.BlobPurgeAt(expiresAt); blobPurgeAt.After(purgeAt) {
			purgeAt = blobPurgeAt
		}
	}
	return purgeAt
}
