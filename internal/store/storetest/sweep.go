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
	t, ctx := f.t, f.t.Context()
	created := f.create(1, oneMinute, false)
	if report, err := f.sweep(f.blobs, baseTime.Add(61*time.Second)); err != nil || report.Blobs != 0 {
		t.Fatalf("the sweep must wait for blob_purge_at: %+v %v", report, err)
	}
	if _, err := f.claim(created, baseTime.Add(61*time.Second), 1); goneState(err) != store.StateExpired {
		t.Fatalf("a claim on an expired message the sweep has not reached: %v", err)
	}
	if report, err := f.sweep(f.blobs, baseTime.Add(store.BlobPurgeStep)); err != nil || report.Blobs != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if record := f.get(created); record.State(baseTime.Add(time.Hour)) != store.StateExpired || record.HasBlob() ||
		f.blobExists(created.ref()) || !record.EndedAt(baseTime.Add(time.Hour)).Equal(created.Message.ExpiresAt) {
		t.Fatalf("expected an expired message without its blob, got %+v", record.Message)
	}
	purgeAt := baseTime.Add(store.TombstoneTTL + time.Hour)
	if report, err := f.sweep(f.blobs, purgeAt.Add(-time.Second)); err != nil || report.Rows != 0 {
		t.Fatalf("an early row purge: %+v %v", report, err)
	}
	if report, err := f.sweep(f.blobs, purgeAt); err != nil || report.Rows != 1 {
		t.Fatalf("row purge: %+v %v", report, err)
	}
	if _, err := f.store.Load(ctx, created.Row.Locator); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("after the purge: %v", err)
	}
}

func (f *fixture) sweepSurvivesBlobErrors() {
	t := f.t
	bad := f.create(1, oneMinute, false)
	good := f.create(1, oneMinute, false)
	window := baseTime.Truncate(time.Hour)
	hit := store.RateHit{Key: "k", WindowStart: window, PurgeAt: window.Add(time.Minute)}
	if _, err := f.store.RateHit(t.Context(), []store.RateHit{hit}); err != nil {
		t.Fatal(err)
	}
	due := baseTime.Add(store.BlobPurgeStep)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDelete(bad.ref(), ErrInjected)
	report, err := f.sweep(failing, due)
	if !errors.Is(err, ErrInjected) || report.Blobs != 1 || report.Counters != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	if record := f.get(bad); record.HasBlob() || record.Row.BlobRef != bad.ref() || !f.blobExists(bad.ref()) {
		t.Fatal("a blob whose delete failed must be unreadable and keep its blob_ref for the next sweep")
	}
	if f.get(good).Row.BlobRef != "" || f.blobExists(good.ref()) {
		t.Fatal("one failing blob blocked the rest of the page")
	}
	report, err = f.sweep(f.blobs, due.Add(time.Minute))
	if err != nil || report.Blobs != 1 || f.blobExists(bad.ref()) {
		t.Fatalf("the next sweep must retry: %+v %v", report, err)
	}
}

func (f *fixture) sweepPagesPastFailures() {
	t := f.t
	rows := make([]*message, 0, store.SweepBatch+1)
	for range store.SweepBatch + 1 {
		rows = append(rows, f.createRow(1, oneMinute, false))
	}
	slices.SortFunc(rows, func(first, second *message) int {
		return bytes.Compare(first.Row.Locator[:], second.Row.Locator[:])
	})
	failingRefs := make([]string, 0, store.SweepBatch)
	for _, row := range rows[:store.SweepBatch] {
		failingRefs = append(failingRefs, row.ref())
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	failing := NewFaultyBlobs(f.blobs)
	for _, ref := range failingRefs {
		failing.FailDelete(ref, ErrInjected)
	}
	report, err := store.SweepAll(ctx, f.store, failing, baseTime.Add(store.BlobPurgeStep))
	if !errors.Is(err, ErrInjected) || report.Blobs != 1 || f.get(rows[store.SweepBatch]).Row.BlobRef != "" {
		t.Fatalf("a full page of failures must not hide the rows behind it: %+v", report)
	}
}

func (f *fixture) sweepDeletesAPageTogether() {
	t := f.t
	unspent := f.create(2, oneMinute, false)
	spent := f.create(1, oneMinute, false)
	if _, err := f.claim(spent, baseTime, 1); err != nil {
		t.Fatal(err)
	}
	burned := f.create(1, oneMinute, false)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(burned.ref(), ErrInjected)
	if _, err := f.end(burned, store.StateDeleted, baseTime, failing); !errors.Is(err, ErrInjected) {
		t.Fatal(err)
	}
	live := f.create(1, oneHour, false)
	recorder := &batchRecorder{Blobs: f.blobs, store: f.store, rows: map[string]*message{}}
	for _, created := range []*message{unspent, spent, burned} {
		recorder.rows[created.ref()] = created
	}
	report, err := f.sweep(recorder, baseTime.Add(store.BlobPurgeStep))
	if err != nil || report.Blobs != 3 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if recorder.batches != 1 || len(recorder.refs) != 3 || recorder.deletes != 0 || len(recorder.readable) != 0 {
		t.Fatalf("a page must lose its blob keys first and then delete its blobs in one batch: %+v", recorder)
	}
	f.checkSweptPage(unspent, spent, burned)
	if record := f.get(live); !record.HasBlob() || !f.blobExists(live.ref()) {
		t.Fatal("the sweep deleted a live blob")
	}
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
		if record := f.get(created); record.State(later) != want.state || !record.EndedAt(later).Equal(want.ended) ||
			f.blobExists(created.ref()) {
			f.t.Fatalf("want a %s message ended at %v and no blob: %+v", want.state, want.ended, record.Message)
		}
	}
}

func (f *fixture) sweepPurgesInBatches() {
	t := f.t
	hits := make([]store.RateHit, 0, 2500)
	for i := range 2500 {
		hits = append(hits, store.RateHit{Key: fmt.Sprintf("k%d", i), WindowStart: baseTime, PurgeAt: baseTime})
	}
	if _, err := f.store.RateHit(t.Context(), hits); err != nil {
		t.Fatal(err)
	}
	if report, err := f.sweep(f.blobs, baseTime); err != nil || report.Counters != 2500 {
		t.Fatalf("batched purge: %+v %v", report, err)
	}
}

func (f *fixture) reconcile() {
	t, ctx := f.t, f.t.Context()
	fresh, failingA, failingB, orphan := core.NewID().String(), core.NewID().String(), core.NewID().String(),
		core.NewID().String()
	for _, ref := range []string{fresh, failingA, failingB, orphan} {
		if err := f.blobs.Put(ctx, ref, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	live := f.create(1, oneHour, false)
	report, err := store.Reconcile(ctx, f.store, f.blobs, time.Now())
	if err != nil || !report.Listed || report.Orphans != 0 || !f.blobExists(fresh) {
		t.Fatalf("a reconcile inside the grace window must keep fresh objects: %+v %v", report, err)
	}
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDelete(failingA, ErrInjected)
	failing.FailDelete(failingB, ErrInjected)
	report, err = store.Reconcile(ctx, f.store, failing, time.Now().Add(store.BlobGrace+store.BlobPurgeStep))
	if !errors.Is(err, ErrInjected) || !report.Listed || report.Orphans != 2 || f.blobExists(fresh) ||
		f.blobExists(orphan) {
		t.Fatalf("a failing delete must not stop the others: %+v %v", report, err)
	}
	if failing.DeleteAttempts(failingA) != 1 || failing.DeleteAttempts(failingB) != 1 || !f.blobExists(live.ref()) {
		t.Fatal("the reconcile skipped an orphan after a failure or deleted a live blob")
	}
}
