package storetest

import (
	"bytes"
	"errors"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) createGetConflict() {
	t, ctx := f.t, f.t.Context()
	message := f.create(1, oneHour, false)
	record := f.get(message.ID)
	if record.Message == nil || record.Message.OpensRemaining != 1 || record.Message.State != store.StateAvailable ||
		!bytes.Equal(record.Message.ReadMAC, message.ReadMAC) || record.Message.PepperID != 7 {
		t.Fatalf("unexpected record %+v", record.Message)
	}
	if err := f.store.Create(ctx, message); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	if _, err := f.store.Get(ctx, core.NewID().String()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := f.end(message.ID, store.StateCancelled, baseTime); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Create(ctx, message); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("create over a tombstone: %v", err)
	}
	if tombstone := f.tombstone(message.ID); tombstone.State != store.StateCancelled ||
		tombstone.PurgeAt != baseTime.Add(store.TombstoneTTL) {
		t.Fatalf("unexpected tombstone %+v", tombstone)
	}
}

func (f *fixture) metadataDoesNotConsume() {
	t := f.t
	message := f.create(1, oneHour, false)
	for range 3 {
		if record := f.get(message.ID); record.Message.OpensRemaining != 1 ||
			record.Message.State != store.StateAvailable {
			t.Fatal("reading the metadata changed the message")
		}
		if ref, err := f.store.ReleaseIfDone(t.Context(), message.ID, baseTime); err != nil || ref != "" {
			t.Fatalf("release of a live message: %q %v", ref, err)
		}
	}
	if !f.blobExists(message.ID) {
		t.Fatal("the blob vanished")
	}
}

func (f *fixture) concurrentClaims() {
	t := f.t
	for iteration := range 100 {
		message := f.create(1, oneHour, false)
		var wait sync.WaitGroup
		results := make([]error, 2)
		for i := range results {
			wait.Go(func() { _, results[i] = f.claim(message, baseTime, i) })
		}
		wait.Wait()
		opened, gone := 0, 0
		for _, err := range results {
			switch {
			case err == nil:
				opened++
			case goneState(err) == store.StateOpened:
				gone++
			default:
				t.Fatalf("iteration %d: unexpected error %v", iteration, err)
			}
		}
		if opened != 1 || gone != 1 {
			t.Fatalf("iteration %d: %d opened, %d gone", iteration, opened, gone)
		}
		if record := f.get(message.ID); record.Message.OpensRemaining != 0 ||
			record.Message.State != store.StateOpened || record.Message.FirstOpenedAt == nil {
			t.Fatalf("iteration %d: state after the claims %+v", iteration, record.Message)
		}
	}
}

func (f *fixture) passphraseBackoff() {
	t := f.t
	message := f.create(2, oneDay, true)
	now := baseTime
	expectRefusal := func(seed int, retryAfter time.Duration, attemptsLeft int) {
		t.Helper()
		_, err := f.claimWithVerifier(message, now, seed, false)
		refusal, ok := errors.AsType[*store.PassphraseError](err)
		if !ok || refusal.RetryAfter != retryAfter || refusal.AttemptsLeft != attemptsLeft {
			t.Fatalf("got %v, want retry after %s with %d attempts left", err, retryAfter, attemptsLeft)
		}
	}
	for attempt := 1; attempt <= store.FreeAttempts; attempt++ {
		expectRefusal(1, 0, store.FreeAttempts-attempt)
	}
	expectRefusal(1, time.Minute, 0)
	expectRefusal(2, time.Minute, 0)
	for range 4 {
		now = now.Add(time.Minute)
		expectRefusal(1, time.Minute, 0)
	}
	if record := f.get(message.ID); record.Message.FailCountTotal != 10 || record.Message.OpensRemaining != 2 {
		t.Fatalf("after ten failures: %+v", record.Message)
	}
	if _, err := f.claimWithVerifier(message, now, 2, true); err == nil {
		t.Fatal("the backoff belongs to the message, so every reader waits, even with the right passphrase")
	}
	if _, err := f.claimWithVerifier(message, now.Add(time.Minute), 2, true); err != nil {
		t.Fatalf("after the backoff: %v", err)
	}
	if record := f.get(message.ID); record.Message.OpensRemaining != 1 || record.Message.FailCountTotal != 10 {
		t.Fatalf("after the right passphrase: %+v", record.Message)
	}
	missing := f.create(1, oneDay, true)
	if _, err := f.claim(missing, baseTime, 1); err == nil || f.get(missing.ID).Message.FailCountTotal != 1 {
		t.Fatalf("a claim without a verifier must count as a failure: %v", err)
	}
}

func (f *fixture) lifetimeCap() {
	t := f.t
	message := f.create(1, oneWeek, true)
	now := baseTime
	for attempt := 1; attempt <= store.MaxFailedAttempts; attempt++ {
		_, err := f.claimWithVerifier(message, now, attempt, false)
		refusal, ok := errors.AsType[*store.PassphraseError](err)
		if !ok || refusal.RetryAfter > store.MaxBackoff {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if attempt == 17 && refusal.RetryAfter != store.MaxBackoff {
			t.Fatalf("the 17th failure waits %s, want the cap %s", refusal.RetryAfter, store.MaxBackoff)
		}
		now = now.Add(refusal.RetryAfter)
	}
	later := now.Add(24 * time.Hour)
	_, err := f.claimWithVerifier(message, later, 201, true)
	refusal, ok := errors.AsType[*store.PassphraseError](err)
	if !ok || refusal.RetryAfter != message.ExpiresAt.Sub(later) || refusal.AttemptsLeft != 0 {
		t.Fatalf("after %d failures the message stays locked until it expires: %v", store.MaxFailedAttempts, err)
	}
	if record := f.get(message.ID); record.Message.FailCountTotal != store.MaxFailedAttempts ||
		record.Message.OpensRemaining != 1 {
		t.Fatalf("lifetime cap counters: %+v", record.Message)
	}
}
