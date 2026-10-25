package storetest

import (
	"context"
	"errors"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) crashAroundTheBlobDelete() {
	beforeDelete := f.crashBeforeTheFileIsDeleted()
	beforeForget := f.crashBeforeTheReferenceIsCleared()
	report, err := f.sweep(f.blobs, baseTime.Add(store.BlobPurgeStep))
	if err != nil || report.Blobs != 1 || f.blobExists(beforeDelete.ref()) {
		f.t.Fatalf("the sweep must finish the release: %+v %v", report, err)
	}
	if err = store.DeleteBlob(
		f.t.Context(),
		f.store,
		f.blobs,
		beforeForget.Row.Locator,
		beforeForget.ref(),
	); err != nil {
		f.t.Fatal(err)
	}
	known, err := f.store.KnownBlobRefs(f.t.Context())
	if _, kept := known[beforeForget.ref()]; err != nil || kept {
		f.t.Fatalf("a repeated delete must clear the blob_ref: %v", err)
	}
}

func (f *fixture) crashBeforeTheFileIsDeleted() *message {
	t := f.t
	created := f.create(1, oneHour, false)
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(created.ref())
	claimed, err := f.claim(created, baseTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.ack(created, &claimed.Lease, baseTime, failing); !errors.Is(err, errDisk) {
		t.Fatalf("a failed delete after the last ack must surface: %v", err)
	}
	if f.get(created).HasBlob() || !f.blobExists(created.ref()) {
		t.Fatal("the blob must be unreadable before its file is deleted")
	}
	return created
}

func (f *fixture) crashBeforeTheReferenceIsCleared() *message {
	t, ctx := f.t, f.t.Context()
	created := f.create(1, oneHour, false)
	if err := f.store.ShredBlobs(ctx, []store.DueBlob{{Locator: created.Row.Locator, Ref: created.ref()}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.blobs.Delete(ctx, created.ref()); err != nil {
			t.Fatalf("Blobs.Delete must be idempotent: %v", err)
		}
	}
	if f.get(created).Row.BlobRef != created.ref() {
		t.Fatal("the crashed state must keep the blob_ref")
	}
	return created
}

func (f *fixture) crashBeforeTheEnd() {
	t := f.t
	created := f.create(2, oneHour, false)
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(created.ref())
	if _, err := f.end(created, store.StateCancelled, baseTime, failing); !errors.Is(err, errDisk) {
		t.Fatalf("a failed blob delete must surface: %v", err)
	}
	if record := f.get(created); record.Message.Ending != store.StateCancelled {
		t.Fatalf("the recorded ending must survive the crash: %+v", record.Message)
	}
	if _, err := f.claim(created, baseTime, 1); goneState(err) != store.StateCancelled {
		t.Fatalf("a claim on an interrupted ending: %v", err)
	}
	if state, err := f.end(
		created,
		store.StateDeleted,
		baseTime,
		f.blobs,
	); err != nil ||
		state != store.StateCancelled {
		t.Fatalf("a later ending must finish the recorded one: %s %v", state, err)
	}
	if f.blobExists(created.ref()) || f.get(created).State(baseTime) != store.StateCancelled {
		t.Fatal("the later ending left the blob or changed the state")
	}
	swept := f.create(1, oneHour, false)
	failing.failOnce(swept.ref())
	if _, err := f.end(swept, store.StateCancelled, baseTime, failing); !errors.Is(err, errDisk) {
		t.Fatal(err)
	}
	report, err := f.sweep(f.blobs, baseTime.Add(time.Second))
	if err != nil || report.Blobs != 1 || f.blobExists(swept.ref()) {
		t.Fatalf("the sweep must finish an interrupted ending: %+v %v", report, err)
	}
}

func (f *fixture) failedDeleteLeavesNothingToRead() {
	t := f.t
	created := f.create(1, oneHour, false)
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(created.ref())
	if _, err := f.end(created, store.StateCancelled, baseTime, failing); !errors.Is(err, errDisk) {
		t.Fatalf("a failed blob delete must surface: %v", err)
	}
	record := f.get(created)
	if _, err := record.BlobKey(); !errors.Is(err, store.ErrBlobMissing) || !f.blobExists(created.ref()) {
		t.Fatalf("after a failed delete the file may stay, but its key must be gone: %v", err)
	}
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	if state, err := store.End(gone, f.store, f.blobs, record, store.StateDeleted, baseTime); err != nil ||
		state != store.StateCancelled {
		t.Fatalf("End must finish the recorded ending even when the caller has gone: %s %v", state, err)
	}
	if f.blobExists(created.ref()) {
		t.Fatal("the retry left the blob")
	}
}

func (f *fixture) recordedEndingWins() {
	t := f.t
	for _, recorded := range []store.State{store.StateDeleted, store.StateCancelled} {
		f.recordedEndingHoldsTheLease(recorded)
	}
	f.recordedReleaseEndsTheMessage()
	opened := f.create(1, oneHour, false)
	f.claimAndAck(opened)
	if state, err := f.end(opened, store.StateCancelled, baseTime, f.blobs); err != nil || state != store.StateOpened {
		t.Fatalf("a cancel after the last release must report opened: %s %v", state, err)
	}
	if record := f.get(opened); record.Message.Ending != "" || record.State(baseTime) != store.StateOpened {
		t.Fatalf("a cancel after the release must record nothing: %+v", record.Message)
	}
}

// recordedReleaseEndsTheMessage is a regression: a release whose blob was not yet deleted once let a burn or a report in.
func (f *fixture) recordedReleaseEndsTheMessage() {
	t, ctx := f.t, f.t.Context()
	created := f.create(1, oneHour, false)
	if _, err := f.claim(created, baseTime, 1); err != nil {
		t.Fatal(err)
	}
	afterLease := baseTime.Add(store.LeaseDuration + store.LeaseTimerSlack)
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(created.ref())
	if _, err := f.finish(created, afterLease, failing); !errors.Is(err, errDisk) {
		t.Fatalf("a release whose blob file could not be deleted: %v", err)
	}
	record := f.get(created)
	if record.HasBlob() || !record.Ended(afterLease) || !f.blobExists(created.ref()) {
		t.Fatal("the release must end the message and drop the blob key in one transaction, even if the file stays")
	}
	if state, err := store.MarkEnding(ctx, f.store, record, store.StateCancelled, afterLease); err != nil ||
		state != store.StateOpened {
		t.Fatalf("a cancel after a recorded release: %s %v", state, err)
	}
	if _, err := store.CountReport(ctx, f.store, f.get(created), afterLease); goneState(err) != store.StateOpened {
		t.Fatalf("a report after a recorded release: %v", err)
	}
}

func (f *fixture) recordedEndingHoldsTheLease(recorded store.State) {
	t := f.t
	created := f.create(1, oneHour, false)
	claimed, err := f.claim(created, baseTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(created.ref())
	if _, err = f.end(created, recorded, baseTime, failing); !errors.Is(err, errDisk) {
		t.Fatal(err)
	}
	if err = f.download(created, &claimed.Lease, baseTime); goneState(err) != recorded {
		t.Fatalf("a download after a recorded %s must report it: %v", recorded, err)
	}
	if err = f.ack(created, &claimed.Lease, baseTime, f.blobs); goneState(err) != recorded {
		t.Fatalf("an ack after a recorded %s must report it: %v", recorded, err)
	}
	if finished, finishErr := f.finish(created, baseTime, f.blobs); finishErr != nil || !finished {
		t.Fatalf("finishing: %v %v", finished, finishErr)
	}
	if f.get(created).State(baseTime) != recorded || f.blobExists(created.ref()) {
		t.Fatalf("the release must keep the recorded %s", recorded)
	}
}

func (f *fixture) releaseAfterExpiryRecordsNothing() {
	t := f.t
	created := f.create(1, oneHour, false)
	if _, err := f.claim(created, baseTime.Add(59*time.Minute), 1); err != nil {
		t.Fatal(err)
	}
	late := baseTime.Add(59*time.Minute + store.LeaseDuration + store.LeaseTimerSlack)
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(created.ref())
	if _, err := f.finish(created, late, failing); !errors.Is(err, errDisk) {
		t.Fatalf("release: %v", err)
	}
	if record := f.get(created); record.Message.Ending != "" || !record.Message.EndedAt.IsZero() {
		t.Fatalf("an expired message records nothing: %+v", record.Message)
	}
	if finished, err := f.finish(created, late, f.blobs); err != nil || !finished {
		t.Fatalf("release retry: %v %v", finished, err)
	}
	if record := f.get(created); record.State(late) != store.StateOpened ||
		!record.EndedAt(late).Equal(created.Message.ExpiresAt) {
		t.Fatalf("an opened message whose lease ran past the expiry ends at expires_at: %+v", record.Message)
	}
}

func (f *fixture) burnAndCancel() {
	t := f.t
	burned := f.create(1, oneHour, false)
	if _, err := f.end(burned, store.StateDeleted, baseTime, f.blobs); err != nil {
		t.Fatal(err)
	}
	if record := f.get(burned); record.State(baseTime) != store.StateDeleted ||
		record.Message.FirstOpenedAt != nil || f.blobExists(burned.ref()) {
		t.Fatalf("burn: %+v", record.Message)
	}
	cancelled := f.create(2, oneHour, false)
	claimed, err := f.claim(cancelled, baseTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.end(cancelled, store.StateCancelled, baseTime.Add(time.Second), f.blobs); err != nil {
		t.Fatal(err)
	}
	if err = f.download(
		cancelled,
		&claimed.Lease,
		baseTime.Add(2*time.Second),
	); goneState(
		err,
	) != store.StateCancelled {
		t.Fatalf("a lease survived the cancel: %v", err)
	}
	if record := f.get(
		cancelled,
	); record.State(baseTime) != store.StateCancelled ||
		record.Message.FirstOpenedAt == nil {
		t.Fatalf("cancel: %+v", record.Message)
	}
	if state, endErr := f.end(
		cancelled,
		store.StateDeleted,
		baseTime,
		f.blobs,
	); endErr != nil ||
		state != store.StateCancelled {
		t.Fatalf("ending twice must report the first ending: %s %v", state, endErr)
	}
	f.endedTimes()
}

func (f *fixture) endedTimes() {
	t := f.t
	late := f.create(1, oneHour, false)
	if _, err := f.end(
		late,
		store.StateDeleted,
		baseTime.Add(95*time.Second+300*time.Millisecond),
		f.blobs,
	); err != nil {
		t.Fatal(err)
	}
	if record := f.get(late); !record.Message.EndedAt.Equal(baseTime.Add(time.Minute)) ||
		!record.Row.PurgeAt.Equal(baseTime.Add(store.TombstoneTTL+time.Hour)) {
		t.Fatalf(
			"ended_at is rounded down to the minute and purge_at up to the hour: %+v %+v",
			record.Message,
			record.Row,
		)
	}
	expired := f.create(1, oneHour, false)
	finishedAt := baseTime.Add(time.Hour + 4*time.Minute + 30*time.Second)
	if state, err := f.end(
		expired,
		store.StateDeleted,
		finishedAt,
		f.blobs,
	); err != nil ||
		state != store.StateExpired {
		t.Fatalf("a burn after the expiry: %s %v", state, err)
	}
	if record := f.get(expired); !record.EndedAt(finishedAt).Equal(expired.Message.ExpiresAt) ||
		!record.Message.EndedAt.IsZero() {
		t.Fatalf("an expired message ends at expires_at and records nothing: %+v", record.Message)
	}
}

func (f *fixture) takeDownWithoutTheKey() {
	t, ctx := f.t, f.t.Context()
	created := f.create(2, oneHour, false)
	removal, err := store.TakeDown(ctx, f.store, f.blobs, created.Row.Locator, baseTime)
	if err != nil || removal != store.Removed || f.blobExists(created.ref()) {
		t.Fatalf("takedown: %v %v", removal, err)
	}
	record := f.get(created)
	if record.State(baseTime) != store.StateDeleted || !record.EndedAt(baseTime).IsZero() ||
		!record.Row.PurgeAt.Equal(baseTime.Add(store.TombstoneTTL)) {
		t.Fatalf("a removed message reads deleted without ended_at and goes after 7 days: %+v", record.Row)
	}
	if _, err = f.claim(created, baseTime, 1); goneState(err) != store.StateDeleted {
		t.Fatalf("a claim after the takedown: %v", err)
	}
	if removal, err = store.TakeDown(ctx, f.store, f.blobs, created.Row.Locator, baseTime); err != nil ||
		removal != store.AlreadyEnded {
		t.Fatalf("a second takedown: %v %v", removal, err)
	}
	unknown := core.NewID()
	if _, err = store.TakeDown(
		ctx,
		f.store,
		f.blobs,
		seal.LocatorOf(unknown[:]),
		baseTime,
	); !errors.Is(
		err,
		store.ErrNotFound,
	) {
		t.Fatalf("takedown of an unknown id: %v", err)
	}
	f.takeDownFinishesThroughTheSweep()
	expired := f.create(1, oneMinute, false)
	dueAt := baseTime.Add(store.BlobPurgeStep)
	if due, dueErr := store.TakeDown(ctx, f.store, f.blobs, expired.Row.Locator, dueAt); dueErr != nil ||
		due != store.AlreadyEnded || f.blobExists(expired.ref()) {
		t.Fatalf("a message already due for the sweep: %v %v", due, dueErr)
	}
	if state := f.get(expired).State(dueAt); state != store.StateExpired {
		t.Fatalf("a takedown must not change how an expired message ended: %s", state)
	}
}

func (f *fixture) takeDownFinishesThroughTheSweep() {
	t := f.t
	stuck := f.create(1, oneHour, false)
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(stuck.ref())
	if _, err := store.TakeDown(t.Context(), f.store, failing, stuck.Row.Locator, baseTime); !errors.Is(err, errDisk) {
		t.Fatalf("a failed delete must surface: %v", err)
	}
	if f.get(stuck).HasBlob() {
		t.Fatal("a takedown must make the blob unreadable before its file is deleted")
	}
	if report, err := f.sweep(f.blobs, baseTime); err != nil || report.Blobs != 1 || f.blobExists(stuck.ref()) {
		t.Fatalf("the sweep must finish the takedown: %+v %v", report, err)
	}
}

func (f *fixture) reportsPerMessageAndDay() {
	t, ctx := f.t, f.t.Context()
	created := f.create(1, oneWeek, false)
	record := f.get(created)
	for report := 1; report <= store.ReportsPerDay; report++ {
		if counted, err := store.CountReport(ctx, f.store, record, baseTime); err != nil || !counted {
			t.Fatalf("report %d: %v %v", report, counted, err)
		}
	}
	if counted, err := store.CountReport(ctx, f.store, record, baseTime); err != nil || counted {
		t.Fatalf("a fourth report on one day: %v %v", counted, err)
	}
	if err := store.UncountReport(ctx, f.store, record, baseTime); err != nil {
		t.Fatal(err)
	}
	if counted, err := store.CountReport(ctx, f.store, record, baseTime); err != nil || !counted {
		t.Fatalf("a report after a count was taken back: %v %v", counted, err)
	}
	if counted, err := store.CountReport(ctx, f.store, record, baseTime.Add(24*time.Hour)); err != nil || !counted {
		t.Fatalf("a report on the next day: %v %v", counted, err)
	}
	if _, err := f.end(created, store.StateDeleted, baseTime.Add(25*time.Hour), f.blobs); err != nil {
		t.Fatal(err)
	}
	record = f.get(created)
	if _, err := store.CountReport(
		ctx,
		f.store,
		record,
		baseTime.Add(26*time.Hour),
	); goneState(
		err,
	) != store.StateDeleted {
		t.Fatalf("a report about an ended message: %v", err)
	}
}
