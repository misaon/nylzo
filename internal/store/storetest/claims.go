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
	ctx := f.t.Context()
	created := f.create(1, oneHour, false)
	record := f.get(created)
	f.fatalIf(record.Message.OpensRemaining != 1 || record.State(baseTime) != store.StateAvailable || !record.HasBlob(),
		"unexpected record %+v", record.Message)
	duplicate := created.Row
	err := f.store.Create(ctx, &duplicate)
	f.fatalIf(!errors.Is(err, store.ErrConflict), "duplicate create: %v", err)
	unknown := core.NewID()
	_, err = f.store.Load(ctx, seal.LocatorOf(unknown[:]))
	f.fatalIf(!errors.Is(err, store.ErrNotFound), "unknown id: %v", err)
	_, err = f.end(created, store.StateCancelled, baseTime, f.blobs)
	f.must(err)
	err = f.store.Create(ctx, &duplicate)
	f.fatalIf(!errors.Is(err, store.ErrConflict), "create over an ended message: %v", err)
	ended := f.get(created)
	f.fatalIf(ended.State(baseTime) != store.StateCancelled ||
		!ended.Row.PurgeAt.Equal(baseTime.Add(store.TombstoneTTL)) || ended.HasBlob(),
		"unexpected ended record %+v %+v", ended.Message, ended.Row)
}

// onlyThePresentedKeyOpens checks that a row opens with pk_R or pk_M for its own purpose, together with the pepper
// key, and with nothing else.
func (f *fixture) onlyThePresentedKeyOpens() {
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
	f.fatalIf(
		open(recordsKey, created.readKey, seal.Read) != nil || open(recordsKey, created.manageKey, seal.Manage) != nil,
		"the read and manage keys must open the record",
	)
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
		f.fatalIf(failure == nil, "%s opened the record", name)
	}
	f.fatalIf(len(row.Record)%seal.RecordBlock != 0 || len(row.ReadWrap) != seal.SealedWrapLen ||
		len(row.ManageWrap) != seal.SealedWrapLen || len(row.BlobWrap) != seal.BlobWrapLen,
		"a sealed row must have fixed lengths: record %d, wraps %d, %d and %d",
		len(row.Record), len(row.ReadWrap), len(row.ManageWrap), len(row.BlobWrap))
}

func openElsewhere(row *store.Row, key seal.Key) error {
	moved := *row
	elsewhere := core.NewID()
	moved.Locator = seal.LocatorOf(elsewhere[:])
	_, err := store.OpenRecord(&moved, key)
	return err
}

func (f *fixture) metadataDoesNotConsume() {
	created := f.create(1, oneHour, false)
	for range 3 {
		record := f.get(created)
		f.fatalIf(record.Message.OpensRemaining != 1 || record.State(baseTime) != store.StateAvailable,
			"reading the metadata changed the message")
		finished, err := f.finish(created, baseTime, f.blobs)
		f.fatalIf(err != nil || finished, "finish on a live message: %v %v", finished, err)
	}
	f.fatalIf(!f.blobExists(created.reference()), "the blob vanished")
}

func (f *fixture) concurrentClaims() {
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
			f.fatalIf(
				err != nil && !endedAs(err, store.StateOpened),
				"iteration %d: unexpected error %v",
				iteration,
				err,
			)
			if err == nil {
				opened++
			} else {
				gone++
			}
		}
		f.fatalIf(opened != 1 || gone != 1, "iteration %d: %d opened, %d gone", iteration, opened, gone)
		record := f.get(created)
		f.fatalIf(record.Message.OpensRemaining != 0 || record.State(baseTime) != store.StateOpened ||
			record.Message.FirstOpenedAt == nil,
			"iteration %d: state after the claims %+v", iteration, record.Message)
	}
}

func (f *fixture) passphraseBackoff() {
	created := f.create(2, oneDay, true)
	now := baseTime
	for counted := 1; counted <= store.FreeAttempts; counted++ {
		_, status, err := f.attempt(created, now)
		f.fatalIf(err != nil || status.RetryAfter != 0 || status.AttemptsLeft != store.FreeAttempts-counted,
			"handshake %d: %+v %v", counted, status, err)
	}
	for range 5 {
		_, status, err := f.attempt(created, now)
		f.fatalIf(err != nil || status.RetryAfter != time.Minute || status.AttemptsLeft != 0,
			"a handshake past the free attempts sets the wait: %+v %v", status, err)
		_, _, err = f.attempt(created, now)
		refusal, ok := errors.AsType[*store.PassphraseError](err)
		f.fatalIf(!ok || refusal.RetryAfter != time.Minute,
			"a handshake during the wait is refused and counts nothing: %v", err)
		now = now.Add(time.Minute)
	}
	record := f.get(created)
	f.fatalIf(record.Message.FailCountTotal != 10 || record.Message.OpensRemaining != 2,
		"after ten counted handshakes: %+v", record.Message)
	_, err := f.claim(created, now, 1)
	f.fatalIf(err != nil || f.get(created).Message.FailCountTotal != 10,
		"a claim counts no attempt; its KE3 has already been checked: %v", err)
}

func (f *fixture) lifetimeCap() {
	created := f.create(1, oneWeek, true)
	now := baseTime
	for counted := 1; counted <= store.MaxFailedAttempts; counted++ {
		_, status, err := f.attempt(created, now)
		f.fatalIf(err != nil || status.RetryAfter > store.MaxBackoff, "handshake %d: %v", counted, err)
		f.fatalIf(counted == 17 && status.RetryAfter != store.MaxBackoff,
			"the 17th handshake waits %s, want the cap %s", status.RetryAfter, store.MaxBackoff)
		now = now.Add(status.RetryAfter)
	}
	later := now.Add(24 * time.Hour)
	_, _, err := f.attempt(created, later)
	refusal, ok := errors.AsType[*store.PassphraseError](err)
	f.fatalIf(!ok || refusal.RetryAfter != created.Message.ExpiresAt.Sub(later) || refusal.AttemptsLeft != 0,
		"after %d handshakes the message stays locked until it expires: %v", store.MaxFailedAttempts, err)
	record := f.get(created)
	f.fatalIf(record.Message.FailCountTotal != store.MaxFailedAttempts || record.Message.OpensRemaining != 1,
		"lifetime cap counters: %+v", record.Message)
}

func (f *fixture) takeBackRestoresOnlyItsOwnWait() {
	created := f.create(3, oneDay, false)
	for range store.FreeAttempts {
		_, _, err := f.attempt(created, baseTime)
		f.must(err)
	}
	honest, status, err := f.attempt(created, baseTime)
	f.fatalIf(err != nil || status.RetryAfter != time.Minute, "the sixth handshake sets a wait: %+v %v", status, err)
	f.takeBack(created, honest, baseTime)
	record := f.get(created)
	f.fatalIf(record.Message.FailCountTotal != store.FreeAttempts || !record.Message.NextAttemptAt.IsZero(),
		"a matching KE3 takes its attempt and its wait back: %+v", record.Message)
	first, _, err := f.attempt(created, baseTime)
	f.must(err)
	later := baseTime.Add(time.Minute)
	_, _, err = f.attempt(created, later)
	f.must(err)
	f.takeBack(created, first, later)
	record = f.get(created)
	f.fatalIf(record.Message.FailCountTotal != store.FreeAttempts+1 || !record.Message.NextAttemptAt.After(later),
		"a wait that a later handshake set stays: %+v", record.Message)
}

// Two handshakes of one reader count the fifth and the sixth attempt; when the fifth one's KE3 matches first, the count
// is back within the free attempts, so the wait that the sixth set goes.
func (f *fixture) takeBackDropsAWaitTheCountNoLongerReaches() {
	created := f.create(1, oneDay, false)
	for range store.FreeAttempts - 1 {
		_, _, err := f.attempt(created, baseTime)
		f.must(err)
	}
	fifth, _, err := f.attempt(created, baseTime)
	f.must(err)
	sixth, status, err := f.attempt(created, baseTime)
	f.fatalIf(err != nil || status.RetryAfter == 0, "the sixth handshake sets a wait: %+v %v", status, err)
	f.takeBack(created, fifth, baseTime)
	record := f.get(created)
	f.fatalIf(record.Message.FailCountTotal != store.FreeAttempts || !record.Message.NextAttemptAt.IsZero(),
		"a wait outlived the count that set it: %+v", record.Message)
	f.takeBack(created, sixth, baseTime)
	record = f.get(created)
	f.fatalIf(record.Message.FailCountTotal != store.FreeAttempts-1 || !record.Message.NextAttemptAt.IsZero(),
		"after both KE3s: %+v", record.Message)
}

func (f *fixture) endedMessagesCountNoAttempt() {
	created := f.create(1, oneDay, true)
	_, err := f.end(created, store.StateDeleted, baseTime, f.blobs)
	f.must(err)
	_, _, err = f.attempt(created, baseTime)
	f.fatalIf(!endedAs(err, store.StateDeleted), "a handshake on an ended message: %v", err)
	expired := f.create(1, oneHour, false)
	_, _, err = f.attempt(expired, baseTime.Add(2*time.Hour))
	f.fatalIf(!endedAs(err, store.StateExpired), "a handshake on an expired message: %v", err)
	f.fatalIf(f.get(created).Message.FailCountTotal != 0 || f.get(expired).Message.FailCountTotal != 0,
		"an ended message counts no attempt")
}
