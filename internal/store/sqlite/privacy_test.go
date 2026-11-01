package sqlite_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	record := newRecord(1, true)
	if err := blobs.Put(t.Context(), record.Row.BlobRef, bytes.NewReader(make([]byte, 64)), 64); err != nil {
		t.Fatal(err)
	}
	create(t, storage, record)
	locator, sealed := record.Row.Locator, slices.Clone(record.Row.Record)
	claimedAt := testNow.Add(time.Minute)
	parameters := claimParams(claimedAt)
	parameters.VerifierSent = true
	parameters.VerifyMAC = func(byte, []byte) bool { return true }
	claimed, err := store.Claim(t.Context(), storage, record, parameters)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Ack(t.Context(), storage, blobs, record, claimed.Lease.MAC, claimedAt); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SweepAll(t.Context(), storage, blobs, testNow.Add(9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Load(t.Context(), locator); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the ended message was not purged: %v", err)
	}
	for _, name := range []string{path, path + "-wal"} {
		data, readErr := os.ReadFile(name)
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, locator[:]) || bytes.Contains(data, sealed[:64]) ||
			bytes.Contains(data, []byte(record.Row.BlobRef)) {
			t.Fatalf("%s still holds a trace of a purged message", filepath.Base(name))
		}
	}
}

// TestTheDatabaseHoldsNoMessageFieldInTheClear covers D101: only the locator, sealed bytes and two rounded times stay readable.
func TestTheDatabaseHoldsNoMessageFieldInTheClear(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	locked := newRecord(1, true)
	create(t, storage, locked)
	attemptAt := testNow.Add(17*time.Second + 123*time.Millisecond)
	parameters := claimParams(attemptAt)
	parameters.VerifierSent = true
	parameters.VerifyMAC = func(byte, []byte) bool { return false }
	for attempt := 1; attempt <= store.FreeAttempts+1; attempt++ {
		if _, err := store.Claim(t.Context(), storage, locked, parameters); !isPassphraseError(err) {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	shared := newRecord(3, false)
	create(t, storage, shared)
	var leaseMACs [][]byte
	for range 3 {
		leaseMACs = append(leaseMACs, claim(t, storage, shared, attemptAt).Lease.MAC)
	}
	storage.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string][]byte{
		"a verifier mac":       testMAC,
		"a lease mac":          leaseMACs[0],
		"the record key":       shared.Key[:],
		"a time to the second": binary.BigEndian.AppendUint64(nil, uint64(testNow.UnixMilli())),
	} {
		if bytes.Contains(data, secret) {
			t.Errorf("the database file holds %s in the clear", name)
		}
	}
	rows, err := openRaw(t, path).Query(`SELECT blob_purge_at, purge_at FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var blobPurgeAt, purgeAt int64
		if err = rows.Scan(&blobPurgeAt, &purgeAt); err != nil {
			t.Fatal(err)
		}
		if blobPurgeAt%store.BlobPurgeStep.Milliseconds() != 0 || purgeAt%store.RowPurgeStep.Milliseconds() != 0 {
			t.Fatalf("a cleanup time is finer than its step: %d %d", blobPurgeAt, purgeAt)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
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
		create(t, storage, newRecord(1, false))
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
	record := newRecord(2, false)
	create(
		t,
		storage,
		record,
		store.RateHit{Key: "global:stored:d", WindowStart: testNow, PurgeAt: testNow.Add(time.Hour)},
	)
	claim(t, storage, record, testNow)
	hit := store.RateHit{Key: "r", WindowStart: testNow, PurgeAt: testNow.Add(time.Hour), Limit: 1}
	errs := []error{}
	_, err := storage.Load(ctx, record.Row.Locator)
	errs = append(errs, err)
	_, err = storage.RateHit(ctx, []store.RateHit{hit})
	errs = append(errs, err)
	_, err = storage.RateReserve(ctx, hit)
	errs = append(errs, err)
	_, err = storage.RateReserveBatch(ctx, []store.RateHit{hit})
	errs = append(errs, err)
	if err = errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	if err = storage.Checkpoint(ctx); err != nil {
		t.Fatalf("a prepared statement kept a read open: %v", err)
	}
}

// TestTheLogKeepsItsBlocksAndHoldsNoOlderRowVersion covers A4-1: a truncated log leaves its frames in freed blocks.
func TestTheLogKeepsItsBlocksAndHoldsNoOlderRowVersion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	var largest int64
	checkLog := func(step string) {
		info, err := os.Stat(path + "-wal")
		if err != nil || info.Size() < largest {
			t.Fatalf("%s: the log shrank from %d bytes, which frees blocks that hold its frames: %v", step, largest, err)
		}
		largest = info.Size()
	}
	hits := make([]store.RateHit, 0, 300)
	for i := range cap(hits) {
		hits = append(hits, store.RateHit{Key: fmt.Sprintf("spend:%03d", i), PurgeAt: testNow.Add(time.Hour)})
	}
	if _, err := storage.RateHit(t.Context(), hits); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		create(t, storage, newRecord(1, false))
	}
	if err := storage.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	record := newRecord(2, false)
	create(t, storage, record, hits...)
	checkLog("create")
	older := slices.Clone(record.Row.Record)
	claim(t, storage, record, testNow)
	checkLog("claim")
	if before, err := os.ReadFile(path + "-wal"); err != nil || !bytes.Contains(before, older[:64]) {
		t.Fatalf("the test needs a stale frame with the older record before the scrub: %v", err)
	}
	if err := storage.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	checkLog("checkpoint")
	for _, name := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, older[:64]) {
			t.Fatalf("%s still holds the record before the claim after the scrub", filepath.Base(name))
		}
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	checkLog("close")
}
