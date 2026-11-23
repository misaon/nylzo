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
	f.fatalIf(err != nil || report.Blobs != 1 || f.blobExists(beforeDelete.reference()),
		"the sweep must finish the release: %+v %v", report, err)
	f.must(store.DeleteBlob(
		f.t.Context(),
		f.store,
		f.blobs,
		beforeForget.Row.Locator,
		beforeForget.reference(),
	))
	known, err := f.store.KnownBlobReferences(f.t.Context())
	_, kept := known[beforeForget.reference()]
	f.fatalIf(err != nil || kept, "a repeated delete must clear the blob_ref: %v", err)
}

func (f *fixture) crashBeforeTheFileIsDeleted() *message {
	created := f.create(1, oneHour, false)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(created.reference(), ErrInjected)
	claimed, err := f.claim(created, baseTime, 1)
	f.must(err)
	err = f.ack(created, &claimed, baseTime, failing)
	f.fatalIf(!errors.Is(err, ErrInjected), "a failed delete after the last ack must surface: %v", err)
	f.fatalIf(f.get(created).HasBlob() || !f.blobExists(created.reference()),
		"the blob must be unreadable before its file is deleted")
	return created
}

func (f *fixture) crashBeforeTheReferenceIsCleared() *message {
	ctx := f.t.Context()
	created := f.create(1, oneHour, false)
	f.must(f.store.ShredBlobs(ctx, []store.DueBlob{{Locator: created.Row.Locator, Reference: created.reference()}}))
	for range 2 {
		err := f.blobs.Delete(ctx, created.reference())
		f.fatalIf(err != nil, "Blobs.Delete must be idempotent: %v", err)
	}
	f.fatalIf(f.get(created).Row.BlobReference != created.reference(), "the crashed state must keep the blob_ref")
	return created
}

func (f *fixture) crashBeforeTheEnd() {
	created := f.create(2, oneHour, false)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(created.reference(), ErrInjected)
	_, err := f.end(created, store.StateCancelled, baseTime, failing)
	f.fatalIf(!errors.Is(err, ErrInjected), "a failed blob delete must surface: %v", err)
	record := f.get(created)
	f.fatalIf(record.Message.Ending != store.StateCancelled,
		"the recorded ending must survive the crash: %+v", record.Message)
	_, err = f.claim(created, baseTime, 1)
	f.fatalIf(!endedAs(err, store.StateCancelled), "a claim on an interrupted ending: %v", err)
	ending, err := f.end(created, store.StateDeleted, baseTime, f.blobs)
	f.fatalIf(err != nil || ending.State != store.StateCancelled || ending.Recorded,
		"a later ending must finish the recorded one: %+v %v", ending, err)
	f.fatalIf(f.blobExists(created.reference()) || f.get(created).State(baseTime) != store.StateCancelled,
		"the later ending left the blob or changed the state")
	swept := f.create(1, oneHour, false)
	failing.FailDeleteOnce(swept.reference(), ErrInjected)
	_, err = f.end(swept, store.StateCancelled, baseTime, failing)
	f.fatalIf(!errors.Is(err, ErrInjected), "%v", err)
	report, err := f.sweep(f.blobs, baseTime.Add(time.Second))
	f.fatalIf(err != nil || report.Blobs != 1 || f.blobExists(swept.reference()),
		"the sweep must finish an interrupted ending: %+v %v", report, err)
}

func (f *fixture) failedDeleteLeavesNothingToRead() {
	created := f.create(1, oneHour, false)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(created.reference(), ErrInjected)
	_, err := f.end(created, store.StateCancelled, baseTime, failing)
	f.fatalIf(!errors.Is(err, ErrInjected), "a failed blob delete must surface: %v", err)
	record := f.get(created)
	_, err = record.BlobKey()
	f.fatalIf(!errors.Is(err, store.ErrBlobMissing) || !f.blobExists(created.reference()),
		"after a failed delete the file may stay, but its key must be gone: %v", err)
	gone, cancel := context.WithCancel(f.t.Context())
	cancel()
	ending, err := store.End(gone, f.store, f.blobs, record, store.StateDeleted, baseTime)
	f.fatalIf(err != nil || ending.State != store.StateCancelled || ending.Recorded,
		"End must finish the recorded ending even when the caller has gone: %+v %v", ending, err)
	f.fatalIf(f.blobExists(created.reference()), "the retry left the blob")
}

func (f *fixture) recordedEndingWins() {
	for _, recorded := range []store.State{store.StateDeleted, store.StateCancelled, store.StateLost} {
		f.recordedEndingHoldsTheLease(recorded)
	}
	f.recordedReleaseEndsTheMessage()
	opened := f.create(1, oneHour, false)
	f.claimAndAck(opened)
	ending, err := f.end(opened, store.StateCancelled, baseTime, f.blobs)
	f.fatalIf(err != nil || ending.State != store.StateOpened,
		"a cancel after the last release must report opened: %+v %v", ending, err)
	record := f.get(opened)
	f.fatalIf(record.Message.Ending != "" || record.State(baseTime) != store.StateOpened,
		"a cancel after the release must record nothing: %+v", record.Message)
}

// recordedReleaseEndsTheMessage is a regression: a release whose blob was not yet deleted once let a burn in.
func (f *fixture) recordedReleaseEndsTheMessage() {
	created := f.create(1, oneHour, false)
	_, err := f.claim(created, baseTime, 1)
	f.must(err)
	afterLease := baseTime.Add(store.LeaseDuration + store.LeaseTimerSlack)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(created.reference(), ErrInjected)
	_, err = f.finish(created, afterLease, failing)
	f.fatalIf(!errors.Is(err, ErrInjected), "a release whose blob file could not be deleted: %v", err)
	record := f.get(created)
	f.fatalIf(record.HasBlob() || !record.Ended(afterLease) || !f.blobExists(created.reference()),
		"the release must end the message and drop the blob key in one transaction, even if the file stays")
	ending, err := store.MarkEnding(f.t.Context(), f.store, record, store.StateCancelled, afterLease)
	f.fatalIf(err != nil || ending.State != store.StateOpened || ending.Recorded,
		"a cancel after a recorded release: %+v %v", ending, err)
}

func (f *fixture) recordedEndingHoldsTheLease(recorded store.State) {
	created := f.create(1, oneHour, false)
	claimed, err := f.claim(created, baseTime, 1)
	f.must(err)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(created.reference(), ErrInjected)
	_, err = f.end(created, recorded, baseTime, failing)
	f.fatalIf(!errors.Is(err, ErrInjected), "%v", err)
	err = f.download(created, &claimed, baseTime)
	f.fatalIf(!endedAs(err, recorded), "a download after a recorded %s must report it: %v", recorded, err)
	err = f.ack(created, &claimed, baseTime, f.blobs)
	f.fatalIf(!endedAs(err, recorded), "an ack after a recorded %s must report it: %v", recorded, err)
	finished, err := f.finish(created, baseTime, f.blobs)
	f.fatalIf(err != nil || !finished, "finishing: %v %v", finished, err)
	f.fatalIf(f.get(created).State(baseTime) != recorded || f.blobExists(created.reference()),
		"the release must keep the recorded %s", recorded)
}

func (f *fixture) releaseAfterExpiryRecordsNothing() {
	created := f.create(1, oneHour, false)
	_, err := f.claim(created, baseTime.Add(59*time.Minute), 1)
	f.must(err)
	late := baseTime.Add(59*time.Minute + store.LeaseDuration + store.LeaseTimerSlack)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(created.reference(), ErrInjected)
	_, err = f.finish(created, late, failing)
	f.fatalIf(!errors.Is(err, ErrInjected), "release: %v", err)
	record := f.get(created)
	f.fatalIf(record.Message.Ending != "" || !record.Message.EndedAt.IsZero(),
		"an expired message records nothing: %+v", record.Message)
	finished, err := f.finish(created, late, f.blobs)
	f.fatalIf(err != nil || !finished, "release retry: %v %v", finished, err)
	record = f.get(created)
	f.fatalIf(record.State(late) != store.StateOpened || !record.EndedAt(late).Equal(created.Message.ExpiresAt),
		"an opened message whose lease ran past the expiry ends at expires_at: %+v", record.Message)
}

// A second ending of one message, as from a concurrent request, reports the recorded state and records nothing, so
// the ended messages are counted once.
func (f *fixture) secondEndingRecordsNothing() {
	burned := f.create(1, oneHour, false)
	ending, err := f.end(burned, store.StateDeleted, baseTime, f.blobs)
	f.fatalIf(err != nil || !ending.Recorded, "burn: %+v %v", ending, err)
	again, err := f.end(burned, store.StateDeleted, baseTime, f.blobs)
	f.fatalIf(err != nil || again != (store.Ending{State: store.StateDeleted}),
		"a second burn recorded the ending again: %+v %v", again, err)
}

func (f *fixture) burnAndCancel() {
	burned := f.create(1, oneHour, false)
	ending, err := f.end(burned, store.StateDeleted, baseTime, f.blobs)
	f.fatalIf(err != nil || !ending.Recorded, "burn: %+v %v", ending, err)
	record := f.get(burned)
	f.fatalIf(record.State(baseTime) != store.StateDeleted || record.Message.FirstOpenedAt != nil ||
		f.blobExists(burned.reference()), "burn: %+v", record.Message)
	cancelled := f.create(2, oneHour, false)
	claimed, err := f.claim(cancelled, baseTime, 1)
	f.must(err)
	_, err = f.end(cancelled, store.StateCancelled, baseTime.Add(time.Second), f.blobs)
	f.must(err)
	err = f.download(cancelled, &claimed, baseTime.Add(2*time.Second))
	f.fatalIf(!endedAs(err, store.StateCancelled), "a lease survived the cancel: %v", err)
	record = f.get(cancelled)
	f.fatalIf(record.State(baseTime) != store.StateCancelled || record.Message.FirstOpenedAt == nil,
		"cancel: %+v", record.Message)
	ending, err = f.end(cancelled, store.StateDeleted, baseTime, f.blobs)
	f.fatalIf(err != nil || ending.State != store.StateCancelled || ending.Recorded,
		"ending twice must report the first ending: %+v %v", ending, err)
	f.endedTimes()
}

func (f *fixture) endedTimes() {
	late := f.create(1, oneHour, false)
	_, err := f.end(late, store.StateDeleted, baseTime.Add(95*time.Second+300*time.Millisecond), f.blobs)
	f.must(err)
	record := f.get(late)
	f.fatalIf(!record.Message.EndedAt.Equal(baseTime.Add(time.Minute)) ||
		!record.Row.PurgeAt.Equal(baseTime.Add(store.TombstoneTTL+time.Hour)),
		"ended_at is rounded down to the minute and purge_at up to the hour: %+v %+v", record.Message, record.Row)
	expired := f.create(1, oneHour, false)
	finishedAt := baseTime.Add(time.Hour + 4*time.Minute + 30*time.Second)
	ending, err := f.end(expired, store.StateDeleted, finishedAt, f.blobs)
	f.fatalIf(err != nil || ending.State != store.StateExpired || ending.Recorded,
		"a burn after the expiry: %+v %v", ending, err)
	record = f.get(expired)
	f.fatalIf(!record.EndedAt(finishedAt).Equal(expired.Message.ExpiresAt) || !record.Message.EndedAt.IsZero(),
		"an expired message ends at expires_at and records nothing: %+v", record.Message)
}

func (f *fixture) takeDownWithoutTheKey() {
	ctx := f.t.Context()
	created := f.create(2, oneHour, false)
	removal, err := store.TakeDown(ctx, f.store, f.blobs, created.Row.Locator, baseTime)
	f.fatalIf(err != nil || removal != store.Removed || f.blobExists(created.reference()),
		"takedown: %v %v", removal, err)
	record := f.get(created)
	f.fatalIf(record.State(baseTime) != store.StateDeleted || !record.EndedAt(baseTime).IsZero() ||
		!record.Row.PurgeAt.Equal(baseTime.Add(store.TombstoneTTL)),
		"a removed message reads deleted without ended_at and goes after 7 days: %+v", record.Row)
	_, err = f.claim(created, baseTime, 1)
	f.fatalIf(!endedAs(err, store.StateDeleted), "a claim after the takedown: %v", err)
	removal, err = store.TakeDown(ctx, f.store, f.blobs, created.Row.Locator, baseTime)
	f.fatalIf(err != nil || removal != store.AlreadyEnded, "a second takedown: %v %v", removal, err)
	unknown := core.NewID()
	_, err = store.TakeDown(ctx, f.store, f.blobs, seal.LocatorOf(unknown[:]), baseTime)
	f.fatalIf(!errors.Is(err, store.ErrNotFound), "takedown of an unknown id: %v", err)
	f.takeDownFinishesThroughTheSweep()
	expired := f.create(1, oneMinute, false)
	dueAt := baseTime.Add(store.BlobPurgeStep)
	due, err := store.TakeDown(ctx, f.store, f.blobs, expired.Row.Locator, dueAt)
	f.fatalIf(err != nil || due != store.AlreadyEnded || f.blobExists(expired.reference()),
		"a message already due for the sweep: %v %v", due, err)
	state := f.get(expired).State(dueAt)
	f.fatalIf(state != store.StateExpired, "a takedown must not change how an expired message ended: %s", state)
}

func (f *fixture) takeDownFinishesThroughTheSweep() {
	stuck := f.create(1, oneHour, false)
	failing := NewFaultyBlobs(f.blobs)
	failing.FailDeleteOnce(stuck.reference(), ErrInjected)
	_, err := store.TakeDown(f.t.Context(), f.store, failing, stuck.Row.Locator, baseTime)
	f.fatalIf(!errors.Is(err, ErrInjected), "a failed delete must surface: %v", err)
	f.fatalIf(f.get(stuck).HasBlob(), "a takedown must make the blob unreadable before its file is deleted")
	report, err := f.sweep(f.blobs, baseTime)
	f.fatalIf(err != nil || report.Blobs != 1 || f.blobExists(stuck.reference()),
		"the sweep must finish the takedown: %+v %v", report, err)
}
