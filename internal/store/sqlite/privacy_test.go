package sqlite_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestPurgedMessageLeavesNoResidue(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "nylzo.db")
	storage, blobs := openStore(t, path), newBlobs(t, directory)
	message := newMessage(1, true)
	message.CreatedAt = time.Date(2026, 10, 22, 12, 34, 56, 789e6, time.UTC)
	message.ExpiresAt = message.CreatedAt.Add(time.Hour)
	if err := blobs.Put(t.Context(), message.ID, bytes.NewReader(make([]byte, 64)), 64); err != nil {
		t.Fatal(err)
	}
	create(t, storage, message)
	claimedAt := message.CreatedAt.Add(time.Minute)
	parameters := claimParams(message, claimedAt)
	parameters.VerifierSent = true
	parameters.VerifyMAC = func(byte, []byte) bool { return true }
	claimed, err := storage.Claim(t.Context(), parameters)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AckLease(t.Context(), storage, blobs, claimed.Lease, claimedAt); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SweepAll(t.Context(), storage, blobs, message.CreatedAt.Add(9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Get(t.Context(), message.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the tombstone was not purged: %v", err)
	}
	for _, name := range []string{path, path + "-wal"} {
		data, readErr := os.ReadFile(name)
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, []byte(message.ID)) {
			t.Fatalf("%s still holds the id of a purged message", filepath.Base(name))
		}
	}
}

func TestStoredTimesAndRowIDsRevealNoOrder(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	locked := newMessage(1, true)
	create(t, storage, locked)
	attemptAt := testNow.Add(17*time.Second + 123*time.Millisecond)
	parameters := claimParams(locked, attemptAt)
	parameters.VerifierSent = true
	parameters.VerifyMAC = func(byte, []byte) bool { return false }
	database := openRaw(t, path)
	for attempt := 1; attempt <= store.FreeAttempts+1; attempt++ {
		if _, err := storage.Claim(t.Context(), parameters); !isPassphraseError(err) {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		var next int64
		if err := database.QueryRow(`SELECT next_attempt_at FROM messages WHERE id = ?`, locked.ID).
			Scan(&next); err != nil {
			t.Fatal(err)
		}
		free := attempt <= store.FreeAttempts
		if free && next != 0 || !free && (next%time.Minute.Milliseconds() != 0 || next <= attemptAt.UnixMilli()) {
			t.Fatalf("attempt %d stored next_attempt_at %d, which reveals the attempt time", attempt, next)
		}
	}
	shared := newMessage(3, false)
	create(t, storage, shared)
	for range 3 {
		if rowID := claim(t, storage, shared, testNow).Lease.RowID; rowID < 1<<32 {
			t.Fatalf("lease row id %d looks sequential", rowID)
		}
	}
}

func isPassphraseError(err error) bool {
	_, ok := errors.AsType[*store.PassphraseError](err)
	return ok
}

func TestWALHoldsOnlyTheLastTransaction(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	for range 5 {
		create(t, storage, newMessage(1, false))
	}
	wal, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if commits := walCommits(wal); commits > 1 {
		t.Fatalf("the WAL keeps %d transactions, so it shows the order of earlier writes", commits)
	}
}

func walCommits(wal []byte) int {
	const header, frameHeader = 32, 24
	if len(wal) < header {
		return 0
	}
	pageSize := int(binary.BigEndian.Uint32(wal[8:12]))
	salts := wal[16:24]
	commits := 0
	for offset := header; offset+frameHeader+pageSize <= len(wal); offset += frameHeader + pageSize {
		frame := wal[offset : offset+frameHeader]
		if bytes.Equal(frame[8:16], salts) && binary.BigEndian.Uint32(frame[4:8]) != 0 {
			commits++
		}
	}
	return commits
}

func TestABlockedCheckpointFailsTheSweepQuickly(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "nylzo.db")
	storage, blobs := openStore(t, path), newBlobs(t, directory)
	hitCounter(t, storage, "a")
	reading, err := openRaw(t, path).Begin()
	if err != nil {
		t.Fatal(err)
	}
	var counters int
	if err = reading.QueryRow(`SELECT COUNT(*) FROM rate_counters`).Scan(&counters); err != nil {
		t.Fatal(err)
	}
	hitCounter(t, storage, "after the reader")
	start := time.Now()
	if err = storage.Checkpoint(t.Context()); !errors.Is(err, sqlite.ErrCheckpointBusy) {
		t.Fatalf("a checkpoint with a reader holding the WAL: %v", err)
	}
	if held := time.Since(start); held > 2*time.Second {
		t.Fatalf("a blocked checkpoint held the only writer for %v", held)
	}
	if _, err = store.SweepAll(t.Context(), storage, blobs, time.Now()); !errors.Is(err, sqlite.ErrCheckpointBusy) {
		t.Fatalf("a sweep with a reader holding the WAL: %v", err)
	}
	reading.Rollback()
	if _, err = store.SweepAll(t.Context(), storage, blobs, time.Now()); err != nil {
		t.Fatalf("a sweep after the reader left: %v", err)
	}
	locking, err := openRaw(t, path+"?_txlock=immediate").Begin()
	if err != nil {
		t.Fatal(err)
	}
	released := time.AfterFunc(time.Second, func() { locking.Rollback() })
	defer released.Stop()
	hitCounter(t, storage, "b")
}

func TestPreparedStatementsReleaseTheLog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage, ctx := openStore(t, path), t.Context()
	message := newMessage(2, false)
	create(
		t,
		storage,
		message,
		store.RateHit{Key: "global:stored:d", WindowStart: testNow, PurgeAt: testNow.Add(time.Hour)},
	)
	claim(t, storage, message, testNow)
	hit := store.RateHit{Key: "r", WindowStart: testNow, PurgeAt: testNow.Add(time.Hour), Limit: 1}
	errs := []error{}
	_, err := storage.Get(ctx, message.ID)
	errs = append(errs, err)
	_, err = storage.Leases(ctx, message.ID)
	errs = append(errs, err)
	_, err = storage.RateHit(ctx, []store.RateHit{hit})
	errs = append(errs, err)
	_, _, err = storage.RateReserve(ctx, hit)
	errs = append(errs, err)
	_, err = storage.RateReserveBatch(ctx, []store.RateHit{hit})
	errs = append(errs, err)
	if err = errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	if err = storage.Checkpoint(ctx); err != nil {
		t.Fatalf("a prepared statement kept a read open: %v", err)
	}
	if info, statErr := os.Stat(path + "-wal"); statErr != nil || info.Size() != 0 {
		t.Fatalf("the log was not truncated: %v %v", info, statErr)
	}
}
