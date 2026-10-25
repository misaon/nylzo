package storetest

import (
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
	t := f.t
	message := f.create(1, oneMinute, false)
	if report, err := f.sweep(f.blobs, baseTime.Add(59*time.Second)); err != nil || report.Expired != 0 {
		t.Fatalf("an early sweep: %+v %v", report, err)
	}
	if report, err := f.sweep(f.blobs, baseTime.Add(61*time.Second)); err != nil || report.Expired != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if tombstone := f.tombstone(message.ID); tombstone.State != store.StateExpired || f.blobExists(message.ID) {
		t.Fatalf("expected an expired tombstone without its blob, got %+v", tombstone)
	}
	if _, err := f.claim(message, baseTime.Add(2*time.Minute), 1); goneState(err) != store.StateExpired {
		t.Fatalf("a claim on the tombstone: %v", err)
	}
	report, err := f.sweep(f.blobs, baseTime.Add(61*time.Second).Add(store.TombstoneTTL))
	if err != nil || report.Tombstones != 1 {
		t.Fatalf("tombstone purge: %+v %v", report, err)
	}
	if _, err = f.store.Get(t.Context(), message.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("after the purge: %v", err)
	}
	unswept := f.create(1, oneMinute, false)
	if _, err = f.claim(unswept, baseTime.Add(2*time.Minute), 1); goneState(err) != store.StateExpired {
		t.Fatalf("a claim on an expired row the sweep has not reached: %v", err)
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
	report, err := f.sweep(newFailingBlobs(f.blobs, bad.ID), baseTime.Add(2*time.Minute))
	if !errors.Is(err, errDisk) || report.Expired != 1 || report.Counters != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	if record := f.get(bad.ID); record.Message == nil || record.Message.Ending != "" || !f.blobExists(bad.ID) {
		t.Fatal("a row whose blob is still there must stay without a recorded ending")
	}
	if record := f.get(good.ID); record.Tombstone == nil || f.blobExists(good.ID) {
		t.Fatal("one failing blob blocked the rest of the page")
	}
	report, err = f.sweep(f.blobs, baseTime.Add(3*time.Minute))
	if err != nil || report.Expired != 1 || f.blobExists(bad.ID) {
		t.Fatalf("the next sweep must retry: %+v %v", report, err)
	}
}

func (f *fixture) sweepPagesPastFailures() {
	t := f.t
	rows := make([]string, 0, store.SweepBatch+1)
	for range store.SweepBatch + 1 {
		rows = append(rows, f.createRow(oneMinute).ID)
	}
	slices.Sort(rows)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	failing := newFailingBlobs(f.blobs, rows[:store.SweepBatch]...)
	report, err := store.SweepAll(ctx, f.store, failing, baseTime.Add(2*time.Minute))
	if !errors.Is(err, errDisk) || report.Expired != 1 || f.get(rows[store.SweepBatch]).Tombstone == nil {
		t.Fatalf("a full page of failures must not hide the rows behind it: %+v", report)
	}
}

func (f *fixture) sweepEndsAnExpiredPageTogether() {
	t := f.t
	unspent := f.create(2, oneMinute, false)
	spent := f.create(1, oneMinute, false)
	if _, err := f.claim(spent, baseTime, 1); err != nil {
		t.Fatal(err)
	}
	burned := f.create(1, oneMinute, false)
	if _, _, _, err := f.store.MarkEnding(t.Context(), burned.ID, store.StateDeleted, baseTime); err != nil {
		t.Fatal(err)
	}
	live := f.create(1, oneHour, false)
	recorder := &batchRecorder{Blobs: f.blobs, store: f.store}
	report, err := f.sweep(recorder, baseTime.Add(2*time.Minute))
	if err != nil || report.Expired != 3 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if recorder.batches != 1 || len(recorder.refs) != 3 || recorder.deletes != 0 || len(recorder.early) != 0 {
		t.Fatalf("an expired page must delete its blobs in one batch before its rows end: %+v", recorder)
	}
	for message, state := range map[*store.Message]store.State{
		unspent: store.StateExpired,
		spent:   store.StateOpened,
		burned:  store.StateDeleted,
	} {
		if tombstone := f.tombstone(message.ID); tombstone.State != state ||
			!tombstone.EndedAt.Equal(message.ExpiresAt) || f.blobExists(message.ID) {
			t.Fatalf("want a %s tombstone ended at the expiry and no blob: %+v", state, tombstone)
		}
	}
	if record := f.get(live.ID); record.Message == nil || !f.blobExists(live.ID) {
		t.Fatal("the sweep ended a live message")
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
	orphans, listed, err := store.Reconcile(ctx, f.store, f.blobs, time.Now())
	if err != nil || !listed || orphans != 0 || !f.blobExists(fresh) {
		t.Fatalf("a reconcile inside the grace window must keep fresh objects: %d %v", orphans, err)
	}
	failing := newFailingBlobs(f.blobs, failingA, failingB)
	orphans, listed, err = store.Reconcile(ctx, f.store, failing, time.Now().Add(store.BlobGrace+time.Minute))
	if !errors.Is(err, errDisk) || !listed || orphans != 2 || f.blobExists(fresh) || f.blobExists(orphan) {
		t.Fatalf("a failing delete must not stop the others: %d %v %v", orphans, listed, err)
	}
	if failing.attemptsOn(failingA) != 1 || failing.attemptsOn(failingB) != 1 || !f.blobExists(live.ID) {
		t.Fatal("the reconcile skipped an orphan after a failure or deleted a live blob")
	}
}
