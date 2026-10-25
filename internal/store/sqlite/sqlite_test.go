package sqlite_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
	"github.com/misaon/nylzo/internal/store/storetest"
)

const legacyID = "AAAAAAAAAAAAAAAAAAAAAA"

var (
	testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	testMAC = bytes.Repeat([]byte{7}, 32)
)

func TestSQLiteStore(t *testing.T) {
	t.Parallel()
	storetest.Run(t, func(t *testing.T) (store.Store, store.Blobs) {
		t.Helper()
		dir := t.TempDir()
		return openStore(t, filepath.Join(dir, "nylzo.db")), newBlobs(t, dir)
	})
}

func openStore(tb testing.TB, path string) *sqlite.Store {
	tb.Helper()
	opened, err := sqlite.Open(tb.Context(), path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { opened.Close() })
	return opened
}

func newBlobs(t *testing.T, dir string) *disk.Blobs {
	t.Helper()
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return blobs
}

func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newMessage(opens uint16, passphrase bool) *store.Message {
	id := core.NewID().String()
	message := &store.Message{
		ID:             id,
		Version:        1,
		State:          store.StateAvailable,
		CreatedAt:      testNow,
		ExpiresAt:      testNow.Add(time.Hour),
		TTLSeconds:     3600,
		OpensMax:       opens,
		OpensRemaining: opens,
		PepperID:       7,
		ReadMAC:        testMAC,
		ManageMAC:      testMAC,
		Size:           64,
		BlobRef:        id,
	}
	if passphrase {
		message.KDF = 1
		message.VerifierMAC = testMAC
	}
	return message
}

func create(tb testing.TB, s *sqlite.Store, message *store.Message, counters ...store.RateHit) {
	tb.Helper()
	if err := s.Create(tb.Context(), message, counters...); err != nil {
		tb.Fatal(err)
	}
}

func claim(tb testing.TB, s store.Messages, message *store.Message, now time.Time) store.ClaimResult {
	tb.Helper()
	claimed, err := s.Claim(tb.Context(), claimParams(message, now))
	if err != nil {
		tb.Fatal(err)
	}
	return claimed
}

func claimParams(message *store.Message, now time.Time) store.ClaimParams {
	return store.ClaimParams{
		ID:            message.ID,
		Clock:         func() time.Time { return now },
		LeaseMAC:      testMAC,
		LeasePepperID: 7,
	}
}

func hitCounter(t *testing.T, s *sqlite.Store, key string) {
	t.Helper()
	hit := store.RateHit{Key: key, WindowStart: time.UnixMilli(0), PurgeAt: time.UnixMilli(0)}
	if _, err := s.RateHit(t.Context(), []store.RateHit{hit}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentOpenOfANewDatabase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const rounds, roundsAtOnce = 40, 4
	paths := make(chan string, rounds)
	for round := range rounds {
		paths <- filepath.Join(dir, fmt.Sprintf("nylzo-%d.db", round))
	}
	close(paths)
	errs := make([]error, roundsAtOnce)
	var wait sync.WaitGroup
	for worker := range roundsAtOnce {
		wait.Go(func() {
			for path := range paths {
				errs[worker] = errors.Join(errs[worker], openConcurrently(t.Context(), path, 8))
			}
		})
	}
	wait.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
}

func openConcurrently(ctx context.Context, path string, openers int) error {
	errs := make([]error, openers)
	var wait sync.WaitGroup
	for i := range openers {
		wait.Go(func() {
			opened, err := sqlite.Open(ctx, path)
			if err != nil {
				errs[i] = err
				return
			}
			if _, err = opened.Get(ctx, legacyID); err != nil && !errors.Is(err, store.ErrNotFound) {
				errs[i] = err
			}
			errs[i] = errors.Join(errs[i], opened.Close())
		})
	}
	wait.Wait()
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("concurrent open of %s: %w", filepath.Base(path), err)
	}
	return nil
}

func TestOpenGivesUpBehindAHeldWriteLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	openStore(t, path).Close()
	transaction, err := openRaw(t, path).Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err = transaction.Exec(`INSERT INTO rate_counters VALUES ('x', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if opened, openErr := sqlite.Open(t.Context(), path); openErr == nil {
		opened.Close()
		t.Fatal("Open succeeded behind a held write lock")
	}
	// gstack-timing-ok: bounds a retry loop that used to wait ten busy timeouts
	if waited := time.Since(start); waited > 15*time.Second {
		t.Fatalf("Open retried for %v behind a held write lock", waited)
	}
}

func TestOpenStopsWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "new.db")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if opened, err := sqlite.Open(ctx, path); !errors.Is(err, context.Canceled) {
		if err == nil {
			opened.Close()
		}
		t.Fatalf("open with a cancelled context: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a cancelled open left a database file: %v", err)
	}
}

func TestDatabaseFilesArePrivateAndAtTheirPath(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"a#b", "a?b", "a%41b", "a b"} {
		parent := t.TempDir()
		dir := filepath.Join(parent, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "nylzo.db")
		hitCounter(t, openStore(t, path), "k")
		for _, file := range []string{path, path + "-wal", path + "-shm"} {
			info, err := os.Stat(file)
			if err != nil || (info.Size() == 0 && file == path) {
				t.Fatalf("%q: %s was not written at its path: %v", name, filepath.Base(file), err)
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("%s is readable by other users: %v", filepath.Base(file), info.Mode().Perm())
			}
		}
		if entries, _ := os.ReadDir(parent); len(entries) != 1 {
			t.Fatalf("%q: files created outside the data directory: %v", name, entries)
		}
	}
}

type cancelOnDelete struct {
	store.Blobs

	cancel context.CancelFunc
}

func (b cancelOnDelete) Delete(ctx context.Context, ref string) error {
	b.cancel()
	return b.Blobs.Delete(ctx, ref)
}

func TestAnAckFinishesItsEndingAfterTheClientLeft(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, blobs := openStore(t, filepath.Join(dir, "nylzo.db")), newBlobs(t, dir)
	message := newMessage(1, false)
	if err := blobs.Put(t.Context(), message.ID, bytes.NewReader(make([]byte, 64)), 64); err != nil {
		t.Fatal(err)
	}
	create(t, s, message)
	claimed := claim(t, s, message, testNow)
	ctx, cancel := context.WithCancel(t.Context())
	leaving := cancelOnDelete{Blobs: blobs, cancel: cancel}
	if err := store.AckLease(ctx, s, leaving, claimed.Lease, testNow); err != nil {
		t.Fatalf("an ack whose client left after the commit: %v", err)
	}
	if record, err := s.Get(t.Context(), message.ID); err != nil || record.Tombstone == nil {
		t.Fatalf("the ack left the message without its tombstone: %+v %v", record, err)
	}
}
