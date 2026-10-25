package storetest

import (
	"context"
	"errors"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) crashAroundTheBlobDelete() {
	t, ctx := f.t, f.t.Context()
	beforeDelete := f.create(1, oneHour, false)
	beforeClear := f.create(1, oneHour, false)
	for _, message := range []*store.Message{beforeDelete, beforeClear} {
		if ref := f.claimAndAck(message); ref != message.ID {
			t.Fatalf("the ack of the last lease must hand out the blob in its transaction: %q", ref)
		}
		if ref, _ := f.store.ReleaseIfDone(ctx, message.ID, baseTime); ref != message.ID {
			t.Fatal("ReleaseIfDone must return the ref again after a crash")
		}
	}
	for range 2 {
		if err := f.blobs.Delete(ctx, beforeClear.BlobRef); err != nil {
			t.Fatalf("Blobs.Delete must be idempotent: %v", err)
		}
	}
	if !f.blobExists(beforeDelete.ID) || f.get(beforeClear.ID).Message.BlobRef != beforeClear.ID {
		t.Fatal("the crashed state must keep the blob or the blob_ref")
	}
	report, err := f.sweep(f.blobs, baseTime.Add(time.Second))
	if err != nil || report.BlobsReleased != 2 || f.blobExists(beforeDelete.ID) {
		t.Fatalf("the sweep must finish both releases: %+v %v", report, err)
	}
	for _, message := range []*store.Message{beforeDelete, beforeClear} {
		if tombstone := f.tombstone(message.ID); tombstone.State != store.StateOpened {
			t.Fatalf("expected an opened tombstone, got %+v", tombstone)
		}
	}
}

func (f *fixture) crashBeforeTheEnd() {
	t := f.t
	message := f.create(2, oneHour, false)
	f.crashAfterDelete(message, store.StateCancelled)
	if record := f.get(message.ID); record.Message == nil || record.Message.Ending != store.StateCancelled {
		t.Fatalf("the recorded ending must survive the crash: %+v", record.Message)
	}
	if _, err := f.claim(message, baseTime, 1); goneState(err) != store.StateCancelled {
		t.Fatalf("a claim on an interrupted ending: %v", err)
	}
	if state, err := f.end(message.ID, store.StateDeleted, baseTime); err != nil || state != store.StateCancelled {
		t.Fatalf("a later ending must finish the recorded one: %s %v", state, err)
	}
	if tombstone := f.tombstone(message.ID); tombstone.State != store.StateCancelled {
		t.Fatalf("tombstone: %+v", tombstone)
	}
	swept := f.create(1, oneHour, false)
	f.crashAfterDelete(swept, store.StateCancelled)
	burnedThenExpired := f.create(1, oneMinute, false)
	f.crashAfterDelete(burnedThenExpired, store.StateDeleted)
	report, err := f.sweep(f.blobs, baseTime.Add(2*time.Minute))
	if err != nil || report.Resumed != 1 || report.Expired != 1 {
		t.Fatalf("the sweep must finish interrupted endings: %+v %v", report, err)
	}
	if tombstone := f.tombstone(swept.ID); tombstone.State != store.StateCancelled {
		t.Fatalf("swept tombstone: %+v", tombstone)
	}
	if tombstone := f.tombstone(burnedThenExpired.ID); tombstone.State != store.StateDeleted {
		t.Fatalf("a recorded burn must win over a later expiry: %+v", tombstone)
	}
}

func (f *fixture) failedDeleteKeepsTheMessage() {
	t := f.t
	message := f.create(1, oneHour, false)
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(message.ID)
	ending := func(ctx context.Context, state store.State) (store.State, error) {
		return store.EndMessage(ctx, f.store, failing, message.ID, state, baseTime)
	}
	if _, err := ending(t.Context(), store.StateCancelled); !errors.Is(err, errDisk) {
		t.Fatalf("a failed blob delete must surface: %v", err)
	}
	if record := f.get(message.ID); record.Message == nil || record.Message.BlobRef != message.ID ||
		!f.blobExists(message.ID) {
		t.Fatal("a failed blob delete must leave the message live with its blob")
	}
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	if state, err := ending(gone, store.StateDeleted); err != nil || state != store.StateCancelled {
		t.Fatalf("EndMessage must finish the recorded ending even when the caller has gone: %s %v", state, err)
	}
	if tombstone := f.tombstone(message.ID); tombstone.State != store.StateCancelled || f.blobExists(message.ID) {
		t.Fatalf("retry: %+v", tombstone)
	}
}

func (f *fixture) recordedEndingWins() {
	t, ctx := f.t, f.t.Context()
	for _, recorded := range []store.State{store.StateDeleted, store.StateCancelled} {
		f.recordedEndingHoldsTheLease(recorded)
	}
	opened := f.create(1, oneHour, false)
	f.claimAndAck(opened)
	if ref, err := f.store.ReleaseIfDone(ctx, opened.ID, baseTime); err != nil || ref == "" {
		t.Fatalf("release: %q %v", ref, err)
	}
	if state, err := f.end(opened.ID, store.StateCancelled, baseTime); err != nil || state != store.StateOpened {
		t.Fatalf("a cancel racing the last release must report opened: %s %v", state, err)
	}
	if tombstone := f.tombstone(opened.ID); tombstone.State != store.StateOpened || f.blobExists(opened.ID) {
		t.Fatalf("a cancel after the recorded release must leave an opened tombstone: %+v", tombstone)
	}
}

func (f *fixture) recordedEndingHoldsTheLease(recorded store.State) {
	t, ctx := f.t, f.t.Context()
	message := f.create(1, oneHour, false)
	claimed, err := f.claim(message, baseTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = f.store.MarkEnding(ctx, message.ID, recorded, baseTime); err != nil {
		t.Fatal(err)
	}
	if err = f.store.RecordDownload(ctx, claimed.Lease, baseTime); goneState(err) != recorded {
		t.Fatalf("a download after a recorded %s must report it: %v", recorded, err)
	}
	if _, err = f.store.Ack(ctx, claimed.Lease, baseTime); goneState(err) != recorded {
		t.Fatalf("an ack after a recorded %s must report it: %v", recorded, err)
	}
	if state, endErr := f.end(message.ID, store.StateExpired, baseTime); endErr != nil || state != recorded {
		t.Fatalf("finishing: %s %v", state, endErr)
	}
	if tombstone := f.tombstone(message.ID); tombstone.State != recorded || f.blobExists(message.ID) {
		t.Fatalf("the release must keep the recorded %s: %+v", recorded, tombstone)
	}
}

func (f *fixture) releaseAfterExpiryRecordsNothing() {
	t, ctx := f.t, f.t.Context()
	message := f.create(1, oneHour, false)
	if _, err := f.claim(message, baseTime.Add(59*time.Minute), 1); err != nil {
		t.Fatal(err)
	}
	late := baseTime.Add(59*time.Minute + store.LeaseDuration + store.LeaseTimerSlack)
	failing := newFailingBlobs(f.blobs)
	failing.failOnce(message.ID)
	if _, err := store.Release(ctx, f.store, failing, message.ID, late); !errors.Is(err, errDisk) {
		t.Fatalf("release: %v", err)
	}
	if record := f.get(message.ID); record.Message == nil || record.Message.Ending != "" {
		t.Fatalf("an expired row records nothing: %+v", record.Message)
	}
	if released, err := store.Release(ctx, f.store, f.blobs, message.ID, late); err != nil || !released {
		t.Fatalf("release retry: %v %v", released, err)
	}
	if tombstone := f.tombstone(message.ID); tombstone.State != store.StateOpened {
		t.Fatalf("tombstone: %+v", tombstone)
	}
}

func (f *fixture) burnAndCancel() {
	t := f.t
	burned := f.create(1, oneHour, false)
	if _, err := f.end(burned.ID, store.StateDeleted, baseTime); err != nil {
		t.Fatal(err)
	}
	if tombstone := f.tombstone(burned.ID); tombstone.State != store.StateDeleted ||
		tombstone.FirstOpenedAt != nil || f.blobExists(burned.ID) {
		t.Fatalf("burn: %+v", tombstone)
	}
	cancelled := f.create(2, oneHour, false)
	claimed, err := f.claim(cancelled, baseTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.end(cancelled.ID, store.StateCancelled, baseTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	err = f.store.RecordDownload(t.Context(), claimed.Lease, baseTime.Add(2*time.Second))
	if !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("a lease survived the cancel: %v", err)
	}
	if tombstone := f.tombstone(cancelled.ID); tombstone.State != store.StateCancelled ||
		tombstone.FirstOpenedAt == nil {
		t.Fatalf("cancel: %+v", tombstone)
	}
	if _, err = f.end(cancelled.ID, store.StateDeleted, baseTime); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ending twice: %v", err)
	}
	f.endedTimes()
}

func (f *fixture) endedTimes() {
	t := f.t
	late := f.create(1, oneHour, false)
	if _, err := f.end(late.ID, store.StateDeleted, baseTime.Add(95*time.Second+300*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if tombstone := f.tombstone(late.ID); !tombstone.EndedAt.Equal(baseTime.Add(time.Minute)) ||
		!tombstone.PurgeAt.Equal(tombstone.EndedAt.Add(store.TombstoneTTL)) {
		t.Fatalf("ended_at must be rounded down to the minute and purge_at must follow it: %+v", tombstone)
	}
	expired := f.create(1, oneHour, false)
	if _, err := f.end(
		expired.ID,
		store.StateExpired,
		baseTime.Add(time.Hour+4*time.Minute+30*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	recorded := f.create(1, oneHour, false)
	if _, _, _, err := f.store.MarkEnding(t.Context(), recorded.ID, store.StateCancelled, baseTime); err != nil {
		t.Fatal(err)
	}
	if _, err := f.end(recorded.ID, store.StateExpired, baseTime.Add(3*time.Hour+17*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for message, state := range map[*store.Message]store.State{
		expired:  store.StateExpired,
		recorded: store.StateCancelled,
	} {
		if tombstone := f.tombstone(message.ID); tombstone.State != state ||
			!tombstone.EndedAt.Equal(message.ExpiresAt) ||
			!tombstone.PurgeAt.Equal(message.ExpiresAt.Add(store.TombstoneTTL)) {
			t.Fatalf("an ending finished after the expiry ends at expires_at: %+v", tombstone)
		}
	}
}
