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

// onlyThePresentedKeyOpens checks that a row opens with pk_R or pk_M for its own purpose, together with the pepper
// key, and with nothing else.
func (f *fixture) onlyThePresentedKeyOpens() {
	t := f.t
	created := f.create(1, oneHour, false)
	row := f.get(created).Row
	open := func(pepperKey seal.Key, publicKey []byte, purpose seal.Purpose) error {
		wrapped := row.ReadWrap
		if purpose == seal.Manage {
			wrapped = row.ManageWrap
		}
		key, err := seal.UnwrapRecordKey(pepperKey, publicKey, wrapped, row.Locator, purpose)
		if err != nil {
			return err
		}
		_, err = store.OpenRecord(&row, key)
		return err
	}
	if open(recordsKey, created.readKey, seal.Read) != nil || open(recordsKey, created.manageKey, seal.Manage) != nil {
		t.Fatal("the read and manage keys must open the record")
	}
	other := core.NewSecret()[:]
	for name, failure := range map[string]error{
		"another public key":            open(recordsKey, other, seal.Read),
		"the read key for managing":     open(recordsKey, created.readKey, seal.Manage),
		"the manage key for reading":    open(recordsKey, created.manageKey, seal.Read),
		"the read key without pepper":   open(seal.Key{}, created.readKey, seal.Read),
		"the manage key without pepper": open(seal.NewKey(), created.manageKey, seal.Manage),
		"a random record key":           func() error { _, err := store.OpenRecord(&row, seal.NewKey()); return err }(),
		"the record of another locator": openElsewhere(&row, created.Key),
	} {
		if failure == nil {
			t.Fatalf("%s opened the record", name)
		}
	}
	if len(row.Record)%seal.RecordBlock != 0 || len(row.ReadWrap) != seal.SealedWrapLen ||
		len(row.ManageWrap) != seal.SealedWrapLen || len(row.BlobWrap) != seal.BlobWrapLen {
		t.Fatalf("a sealed row must have fixed lengths: record %d, wraps %d, %d and %d",
			len(row.Record), len(row.ReadWrap), len(row.ManageWrap), len(row.BlobWrap))
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
	if !f.blobExists(created.reference()) {
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
	for counted := 1; counted <= store.FreeAttempts; counted++ {
		_, status, err := f.attempt(created, now)
		if err != nil || status.RetryAfter != 0 || status.AttemptsLeft != store.FreeAttempts-counted {
			t.Fatalf("handshake %d: %+v %v", counted, status, err)
		}
	}
	for range 5 {
		_, status, err := f.attempt(created, now)
		if err != nil || status.RetryAfter != time.Minute || status.AttemptsLeft != 0 {
			t.Fatalf("a handshake past the free attempts sets the wait: %+v %v", status, err)
		}
		_, _, err = f.attempt(created, now)
		if refusal, ok := errors.AsType[*store.PassphraseError](err); !ok || refusal.RetryAfter != time.Minute {
			t.Fatalf("a handshake during the wait is refused and counts nothing: %v", err)
		}
		now = now.Add(time.Minute)
	}
	if record := f.get(created); record.Message.FailCountTotal != 10 || record.Message.OpensRemaining != 2 {
		t.Fatalf("after ten counted handshakes: %+v", record.Message)
	}
	if _, err := f.claim(created, now, 1); err != nil || f.get(created).Message.FailCountTotal != 10 {
		t.Fatalf("a claim counts no attempt; its KE3 has already been checked: %v", err)
	}
}

func (f *fixture) lifetimeCap() {
	t := f.t
	created := f.create(1, oneWeek, true)
	now := baseTime
	for counted := 1; counted <= store.MaxFailedAttempts; counted++ {
		_, status, err := f.attempt(created, now)
		if err != nil || status.RetryAfter > store.MaxBackoff {
			t.Fatalf("handshake %d: %v", counted, err)
		}
		if counted == 17 && status.RetryAfter != store.MaxBackoff {
			t.Fatalf("the 17th handshake waits %s, want the cap %s", status.RetryAfter, store.MaxBackoff)
		}
		now = now.Add(status.RetryAfter)
	}
	later := now.Add(24 * time.Hour)
	_, _, err := f.attempt(created, later)
	refusal, ok := errors.AsType[*store.PassphraseError](err)
	if !ok || refusal.RetryAfter != created.Message.ExpiresAt.Sub(later) || refusal.AttemptsLeft != 0 {
		t.Fatalf("after %d handshakes the message stays locked until it expires: %v", store.MaxFailedAttempts, err)
	}
	if record := f.get(created); record.Message.FailCountTotal != store.MaxFailedAttempts ||
		record.Message.OpensRemaining != 1 {
		t.Fatalf("lifetime cap counters: %+v", record.Message)
	}
}

func (f *fixture) takeBackRestoresOnlyItsOwnWait() {
	t := f.t
	created := f.create(3, oneDay, false)
	for range store.FreeAttempts {
		if _, _, err := f.attempt(created, baseTime); err != nil {
			t.Fatal(err)
		}
	}
	honest, status, err := f.attempt(created, baseTime)
	if err != nil || status.RetryAfter != time.Minute {
		t.Fatalf("the sixth handshake sets a wait: %+v %v", status, err)
	}
	f.takeBack(created, honest, baseTime)
	if record := f.get(created); record.Message.FailCountTotal != store.FreeAttempts ||
		!record.Message.NextAttemptAt.IsZero() {
		t.Fatalf("a matching KE3 takes its attempt and its wait back: %+v", record.Message)
	}
	first, _, err := f.attempt(created, baseTime)
	if err != nil {
		t.Fatal(err)
	}
	later := baseTime.Add(time.Minute)
	if _, _, err = f.attempt(created, later); err != nil {
		t.Fatal(err)
	}
	f.takeBack(created, first, later)
	if record := f.get(created); record.Message.FailCountTotal != store.FreeAttempts+1 ||
		!record.Message.NextAttemptAt.After(later) {
		t.Fatalf("a wait that a later handshake set stays: %+v", record.Message)
	}
}

func (f *fixture) endedMessagesCountNoAttempt() {
	t := f.t
	created := f.create(1, oneDay, true)
	if _, err := f.end(created, store.StateDeleted, baseTime, f.blobs); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.attempt(created, baseTime); goneState(err) != store.StateDeleted {
		t.Fatalf("a handshake on an ended message: %v", err)
	}
	expired := f.create(1, oneHour, false)
	if _, _, err := f.attempt(expired, baseTime.Add(2*time.Hour)); goneState(err) != store.StateExpired {
		t.Fatalf("a handshake on an expired message: %v", err)
	}
	if f.get(created).Message.FailCountTotal != 0 || f.get(expired).Message.FailCountTotal != 0 {
		t.Fatal("an ended message counts no attempt")
	}
}
