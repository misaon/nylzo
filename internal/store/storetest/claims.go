package storetest

import (
	"errors"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) createLoadConflict() {
	t, ctx := f.t, f.t.Context()
	created := f.create(1, oneHour, false)
	record := f.get(created)
	if record.Message.OpensRemaining != 1 || record.State(baseTime) != store.StateAvailable || !record.HasBlob() {
		t.Fatalf("unexpected record %+v", record.Message)
	}
	duplicate := created.Row
	if err := f.store.Create(ctx, &duplicate); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	unknown := core.NewID()
	if _, err := f.store.Load(ctx, seal.LocatorOf(unknown[:])); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := f.end(created, store.StateCancelled, baseTime, f.blobs); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Create(ctx, &duplicate); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("create over an ended message: %v", err)
	}
	if ended := f.get(created); ended.State(baseTime) != store.StateCancelled ||
		!ended.Row.PurgeAt.Equal(baseTime.Add(store.TombstoneTTL)) || ended.HasBlob() {
		t.Fatalf("unexpected ended record %+v %+v", ended.Message, ended.Row)
	}
}

// onlyThePresentedKeyOpens checks that a row opens with pk_R or pk_M for its own purpose and with nothing else.
func (f *fixture) onlyThePresentedKeyOpens() {
	t := f.t
	created := f.create(1, oneHour, false)
	row := f.get(created).Row
	open := func(publicKey []byte, purpose seal.Purpose) error {
		wrapped := row.ReadWrap
		if purpose == seal.Manage {
			wrapped = row.ManageWrap
		}
		key, err := seal.Unwrap(seal.KeyFor(publicKey, row.Locator, purpose), wrapped, row.Locator, purpose)
		if err != nil {
			return err
		}
		_, err = store.OpenRecord(&row, key)
		return err
	}
	if open(created.readKey, seal.Read) != nil || open(created.manageKey, seal.Manage) != nil {
		t.Fatal("the read and manage keys must open the record")
	}
	other := core.NewSecret()[:]
	for name, failure := range map[string]error{
		"another public key":            open(other, seal.Read),
		"the read key for managing":     open(created.readKey, seal.Manage),
		"the manage key for reading":    open(created.manageKey, seal.Read),
		"a random record key":           func() error { _, err := store.OpenRecord(&row, seal.NewKey()); return err }(),
		"the record of another locator": openElsewhere(&row, created.Key),
	} {
		if failure == nil {
			t.Fatalf("%s opened the record", name)
		}
	}
	if len(row.Record)%seal.RecordBlock != 0 || len(row.ReadWrap) != seal.WrapLen ||
		len(row.ManageWrap) != seal.WrapLen {
		t.Fatalf("a sealed row must have fixed lengths: record %d, wraps %d and %d",
			len(row.Record), len(row.ReadWrap), len(row.ManageWrap))
	}
}

func openElsewhere(row *store.Row, key seal.Key) error {
	moved := *row
	elsewhere := core.NewID()
	moved.Locator = seal.LocatorOf(elsewhere[:])
	_, err := store.OpenRecord(&moved, key)
	return err
}

func (f *fixture) metadataDoesNotConsume() {
	t := f.t
	created := f.create(1, oneHour, false)
	for range 3 {
		if record := f.get(created); record.Message.OpensRemaining != 1 ||
			record.State(baseTime) != store.StateAvailable {
			t.Fatal("reading the metadata changed the message")
		}
		if finished, err := f.finish(created, baseTime, f.blobs); err != nil || finished {
			t.Fatalf("finish on a live message: %v %v", finished, err)
		}
	}
	if !f.blobExists(created.ref()) {
		t.Fatal("the blob vanished")
	}
}

func (f *fixture) concurrentClaims() {
	t := f.t
	for iteration := range 100 {
		created := f.create(1, oneHour, false)
		var wait sync.WaitGroup
		results := make([]error, 2)
		for i := range results {
			wait.Go(func() { _, results[i] = f.claim(created, baseTime, i) })
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
		if record := f.get(created); record.Message.OpensRemaining != 0 ||
			record.State(baseTime) != store.StateOpened || record.Message.FirstOpenedAt == nil {
			t.Fatalf("iteration %d: state after the claims %+v", iteration, record.Message)
		}
	}
}

func (f *fixture) passphraseBackoff() {
	t := f.t
	created := f.create(2, oneDay, true)
	now := baseTime
	expectRefusal := func(seed int, retryAfter time.Duration, attemptsLeft int) {
		t.Helper()
		_, err := f.claimWithVerifier(created, now, seed, false)
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
	if record := f.get(created); record.Message.FailCountTotal != 10 || record.Message.OpensRemaining != 2 {
		t.Fatalf("after ten failures: %+v", record.Message)
	}
	if _, err := f.claimWithVerifier(created, now, 2, true); err == nil {
		t.Fatal("the backoff belongs to the message, so every reader waits, even with the right passphrase")
	}
	if _, err := f.claimWithVerifier(created, now.Add(time.Minute), 2, true); err != nil {
		t.Fatalf("after the backoff: %v", err)
	}
	if record := f.get(created); record.Message.OpensRemaining != 1 || record.Message.FailCountTotal != 10 {
		t.Fatalf("after the right passphrase: %+v", record.Message)
	}
	missing := f.create(1, oneDay, true)
	if _, err := f.claim(missing, baseTime, 1); err == nil || f.get(missing).Message.FailCountTotal != 1 {
		t.Fatalf("a claim without a verifier must count as a failure: %v", err)
	}
}

func (f *fixture) lifetimeCap() {
	t := f.t
	created := f.create(1, oneWeek, true)
	now := baseTime
	for attempt := 1; attempt <= store.MaxFailedAttempts; attempt++ {
		_, err := f.claimWithVerifier(created, now, attempt, false)
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
	_, err := f.claimWithVerifier(created, later, 201, true)
	refusal, ok := errors.AsType[*store.PassphraseError](err)
	if !ok || refusal.RetryAfter != created.Message.ExpiresAt.Sub(later) || refusal.AttemptsLeft != 0 {
		t.Fatalf("after %d failures the message stays locked until it expires: %v", store.MaxFailedAttempts, err)
	}
	if record := f.get(created); record.Message.FailCountTotal != store.MaxFailedAttempts ||
		record.Message.OpensRemaining != 1 {
		t.Fatalf("lifetime cap counters: %+v", record.Message)
	}
}
