package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

const insertCounter = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES ('k', 0, 1, 0, 0)`

func openTest(t *testing.T) *Store {
	t.Helper()
	opened, err := Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return opened
}

func TestConnectionsKeepTheirCacheAndTemporaryCopiesInMemory(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	for _, database := range []*sql.DB{storage.writer, storage.reader} {
		var cacheKiB, tempStore int
		if err := database.QueryRow("PRAGMA cache_size").Scan(&cacheKiB); err != nil {
			t.Fatal(err)
		}
		if cacheKiB >= 0 || -cacheKiB > maxCacheKiB {
			t.Fatalf("page cache %d, want at most %d KiB per connection outside the Go heap", cacheKiB, maxCacheKiB)
		}
		if err := database.QueryRow("PRAGMA temp_store").Scan(&tempStore); err != nil || tempStore != 2 {
			t.Fatalf(
				"temp_store %d %v: SQLite may write temporary copies of rows outside the data directory",
				tempStore,
				err,
			)
		}
	}
}

func TestMigrationStopsWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", dataSourceName(filepath.Join(t.TempDir(), "nylzo.db"), true))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = migrate(ctx, database); !errors.Is(err, context.Canceled) {
		t.Fatalf("migrate with a cancelled context: %v", err)
	}
}

func TestAPanicInATransactionFreesTheWriter(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	func() {
		defer func() { recover() }()
		storage.transaction(context.WithoutCancel(t.Context()), func(tx *sql.Tx) error {
			if _, err := tx.Exec(insertCounter); err != nil {
				t.Fatal(err)
			}
			panic("test: a bug inside a transaction")
		})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if count, err := storage.RateReserve(ctx, store.RateHit{Key: "k", Limit: 1}); err != nil || count == 0 {
		t.Fatalf("the writer stayed locked after a panic, or the panicking transaction committed: %d %v", count, err)
	}
}

func TestACommitAfterTheClientLeftReportsTheCancellation(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	err := storage.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(insertCounter); err != nil {
			return err
		}
		cancel()
		for {
			if _, err := tx.ExecContext(t.Context(), `SELECT 1`); errors.Is(err, sql.ErrTxDone) {
				return nil
			}
			time.Sleep(time.Millisecond)
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a commit after the client left: %v", err)
	}
}

func TestACorruptRowLeavesTheRestOfItsPageToTheSweep(t *testing.T) {
	t.Parallel()
	storage, ctx := openTest(t), t.Context()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	insert := `INSERT INTO messages (locator, read_wrap, manage_wrap, record, blob_ref, blob_wrap, blob_purge_at, purge_at)
		VALUES (?, x'00', x'00', x'00', ?, x'00', ?, ?)`
	good := bytes.Repeat([]byte{2}, seal.LocatorLen)
	for locator, ref := range map[string]string{string([]byte{1, 2, 3}): "corrupt", string(good): "good"} {
		if _, err := storage.writer.Exec(insert, []byte(locator), ref, now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	page, err := storage.DueBlobs(ctx, now, seal.Locator{}, store.SweepBatch)
	if err != nil || len(page) != 1 || page[0].Ref != "good" {
		t.Fatalf("a row with a corrupt locator must not hide the others: %+v %v", page, err)
	}
	if _, err = storage.Load(ctx, seal.Locator(good)); err != nil {
		t.Fatal(err)
	}
	if purged, purgeErr := storage.PurgeRows(ctx, now, store.SweepBatch); purgeErr != nil || purged != 2 {
		t.Fatalf("the purge must remove a corrupt row too: %d %v", purged, purgeErr)
	}
}
