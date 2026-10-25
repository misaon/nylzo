package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

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

func createTest(t *testing.T, s *Store, id string, ttl time.Duration) {
	t.Helper()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	mac := bytes.Repeat([]byte{7}, 32)
	err := s.Create(t.Context(), &store.Message{
		ID:             id,
		Version:        1,
		State:          store.StateAvailable,
		CreatedAt:      now,
		ExpiresAt:      now.Add(ttl),
		TTLSeconds:     uint32(ttl.Seconds()),
		OpensMax:       1,
		OpensRemaining: 1,
		PepperID:       7,
		ReadMAC:        mac,
		ManageMAC:      mac,
		Size:           64,
		BlobRef:        id,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConnectionsKeepTheirCacheAndTemporaryCopiesInMemory(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	for _, database := range []*sql.DB{s.writer, s.reader} {
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
	database, err := sql.Open("sqlite", dsn(filepath.Join(t.TempDir(), "nylzo.db"), true))
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
	s := openTest(t)
	func() {
		defer func() { recover() }()
		s.transaction(context.WithoutCancel(t.Context()), func(tx *sql.Tx) error {
			if _, err := tx.Exec(insertCounter); err != nil {
				t.Fatal(err)
			}
			panic("test: a bug inside a transaction")
		})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, ok, err := s.RateReserve(ctx, store.RateHit{Key: "k", Limit: 1}); err != nil || !ok {
		t.Fatalf("the writer stayed locked after a panic, or the panicking transaction committed: %v %v", ok, err)
	}
}

func TestACommitAfterTheClientLeftReportsTheCancellation(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	err := s.transaction(ctx, func(tx *sql.Tx) error {
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

func TestImpossibleColumnsAreCorruptRows(t *testing.T) {
	t.Parallel()
	s, ctx := openTest(t), t.Context()
	const spent, corrupt = "AAAAAAAAAAAAAAAAAAAAAA", "AAAAAAAAAAAAAAAAAAAAAQ"
	createTest(t, s, spent, time.Hour)
	createTest(t, s, corrupt, time.Hour)
	if _, err := s.writer.Exec(`UPDATE messages SET opens_remaining = 0 WHERE id = ?`, spent); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	params := store.ClaimParams{
		ID:            spent,
		Clock:         func() time.Time { return now },
		LeaseMAC:      []byte{1},
		LeasePepperID: 7,
	}
	if _, err := s.Claim(ctx, params); goneState(err) != store.StateOpened {
		t.Fatalf("an available row without opens left gave an open: %v", err)
	}
	var remaining int
	err := s.writer.QueryRow(`SELECT opens_remaining FROM messages WHERE id = ?`, spent).Scan(&remaining)
	if err != nil || remaining != 0 {
		t.Fatalf("the counter wrapped: %d %v", remaining, err)
	}
	for _, update := range []string{
		`UPDATE messages SET opens_max = 70000, opens_remaining = 70000 WHERE id = ?`,
		`UPDATE messages SET opens_max = 1, opens_remaining = 1, kdf_id = 257 WHERE id = ?`,
	} {
		if _, err = s.writer.Exec(update, corrupt); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Get(ctx, corrupt); !errors.Is(err, errCorruptRow) {
			t.Fatalf("%s: the row was read: %v", update, err)
		}
	}
	if _, err = s.Claim(ctx, store.ClaimParams{ID: spent}); !errors.Is(err, errClaimParams) {
		t.Fatalf("a claim without a clock: %v", err)
	}
}

func TestACorruptRowLeavesTheRestOfItsPageToEnd(t *testing.T) {
	t.Parallel()
	s, ctx := openTest(t), t.Context()
	ids := []string{"AAAAAAAAAAAAAAAAAAAAAA", "AAAAAAAAAAAAAAAAAAAAAQ"}
	for _, id := range ids {
		createTest(t, s, id, time.Minute)
	}
	if _, err := s.writer.Exec(`UPDATE messages SET opens_remaining = 5 WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	ended, err := s.EndExpired(ctx, ids, time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC))
	if ended != 1 || !errors.Is(err, errCorruptRow) {
		t.Fatalf("end expired: %d %v", ended, err)
	}
	record, err := s.Get(ctx, ids[1])
	if err != nil || record.Tombstone == nil || record.Tombstone.State != store.StateExpired {
		t.Fatalf("the row after a corrupt one did not end: %+v %v", record, err)
	}
}

func goneState(err error) store.State {
	if gone, ok := errors.AsType[*store.GoneError](err); ok {
		return gone.State
	}
	return ""
}
