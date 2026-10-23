package sqlite_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
	"github.com/misaon/nylzo/internal/store/storetest"
)

func TestSQLiteStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) (store.Store, store.Blobs) {
		dir := t.TempDir()
		s, err := sqlite.Open(filepath.Join(dir, "nylzo.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		b, err := disk.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s, b
	})
}

const olderMessages = `CREATE TABLE messages (id TEXT PRIMARY KEY, version INTEGER NOT NULL, kdf_id INTEGER NOT NULL, flags INTEGER NOT NULL, state TEXT NOT NULL,
	created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ttl_s INTEGER NOT NULL, opens_max INTEGER NOT NULL, opens_remaining INTEGER NOT NULL,
	pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, verifier_mac BLOB, size INTEGER NOT NULL, blob_ref TEXT,
	day_fail_count INTEGER NOT NULL DEFAULT 0, day_fail_date TEXT NOT NULL DEFAULT '', fail_count_total INTEGER NOT NULL DEFAULT 0, first_opened_at INTEGER%s);
	%s;
	INSERT INTO messages (id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, size, blob_ref)
	VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 1, 0, 0, 'available', 0, 9999999999999, 60, 1, 1, 7, x'00', x'00', 64, 'AAAAAAAAAAAAAAAAAAAAAA');`

func TestOpenMigratesOlderSchema(t *testing.T) {
	shapes := map[string]string{
		"without ending":  fmt.Sprintf(olderMessages, "", "CREATE INDEX messages_release ON messages(opens_remaining, blob_ref)"),
		"with release_id": fmt.Sprintf(olderMessages, ", ending TEXT", "CREATE INDEX messages_release_id ON messages(opens_remaining, id) WHERE blob_ref IS NOT NULL"),
	}
	for name, ddl := range shapes {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nylzo.db")
			old, err := sql.Open("sqlite", "file:"+path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := old.Exec(ddl); err != nil {
				t.Fatal(err)
			}
			old.Close()
			openConcurrently(t, path, 4)
			s, err := sqlite.Open(path)
			if err != nil {
				t.Fatalf("open an older database: %v", err)
			}
			defer s.Close()
			ctx := t.Context()
			state, ref, _, err := s.MarkEnding(ctx, "AAAAAAAAAAAAAAAAAAAAAA", store.StateCancelled, time.Unix(0, 0))
			if err != nil || state != store.StateCancelled || ref == "" {
				t.Fatalf("mark ending after migration: %s %q %v", state, ref, err)
			}
			rec, err := s.Get(ctx, "AAAAAAAAAAAAAAAAAAAAAA")
			if err != nil || rec.Message == nil || rec.Message.Ending != store.StateCancelled {
				t.Fatalf("get after migration: %+v %v", rec, err)
			}
			check, err := sql.Open("sqlite", "file:"+path)
			if err != nil {
				t.Fatal(err)
			}
			defer check.Close()
			rows, err := check.Query(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'messages' AND sql IS NOT NULL ORDER BY name`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var names []string
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err != nil {
					t.Fatal(err)
				}
				names = append(names, n)
			}
			if got := strings.Join(names, ","); got != "messages_ending,messages_expires_at,messages_releasable" {
				t.Fatalf("message indexes after migration: %s", got)
			}
		})
	}
}

func TestConcurrentOpenOfANewDatabase(t *testing.T) {
	dir := t.TempDir()
	for round := range 40 {
		openConcurrently(t, filepath.Join(dir, fmt.Sprintf("nylzo-%d.db", round)), 8)
	}
}

func openConcurrently(t *testing.T, path string, n int) {
	t.Helper()
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			s, err := sqlite.Open(path)
			if err == nil {
				if _, gerr := s.Get(t.Context(), "AAAAAAAAAAAAAAAAAAAAAA"); gerr != nil && !errors.Is(gerr, store.ErrNotFound) {
					err = gerr
				}
				err = errors.Join(err, s.Close())
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent open of %s: %v", filepath.Base(path), err)
		}
	}
}

func TestLegacyTombstoneEndedAtIsRounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	ended := time.Date(2026, 10, 22, 10, 1, 35, 300e6, time.UTC)
	if _, err := raw.Exec(`INSERT INTO tombstones (id, pepper_id, read_mac, manage_mac, state, ended_at, purge_at) VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 7, x'00', x'00', 'deleted', ?, ?)`, ended.UnixMilli(), ended.Add(store.TombstoneTTL).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	s, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, err := s.Get(t.Context(), "AAAAAAAAAAAAAAAAAAAAAA")
	if err != nil || rec.Tombstone == nil || !rec.Tombstone.EndedAt.Equal(ended.Truncate(time.Minute)) {
		t.Fatalf("a tombstone written before the rounding must be served rounded: %+v %v", rec, err)
	}
}

func TestOpenGivesUpBehindAHeldWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	holder, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	tx, err := holder.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO rate_counters VALUES ('x', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if s, err := sqlite.Open(path); err == nil {
		s.Close()
		t.Fatal("Open succeeded behind a held write lock")
	}
	// gstack-timing-ok: bounds a retry loop that used to wait ten busy timeouts
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("Open retried for %v behind a held write lock", d)
	}
}

func TestDatabaseFilesArePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RateHit(t.Context(), []store.RateHit{{Key: "k", WindowStart: time.Unix(0, 0), PurgeAt: time.Unix(60, 0)}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is readable by other users: %v", filepath.Base(name), info.Mode().Perm())
		}
	}
}
func TestOpenKeepsTheDatabaseAtItsPath(t *testing.T) {
	for _, name := range []string{"a#b", "a?b", "a%41b", "a b"} {
		parent := t.TempDir()
		dir := filepath.Join(parent, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "nylzo.db")
		s, err := sqlite.Open(path)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if _, err := s.RateHit(t.Context(), []store.RateHit{{Key: "k", WindowStart: time.Unix(0, 0), PurgeAt: time.Unix(60, 0)}}); err != nil {
			t.Fatal(err)
		}
		s.Close()
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("%q: database not written at its path: %v", name, err)
		}
		if entries, _ := os.ReadDir(parent); len(entries) != 1 {
			t.Fatalf("%q: files created outside the data directory: %v", name, entries)
		}
	}
}

func TestPurgedMessageLeavesNoResidue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nylzo.db")
	st, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	id := core.NewID().String()
	created := time.Date(2026, 10, 22, 12, 34, 56, 789e6, time.UTC)
	mac := bytes.Repeat([]byte{7}, 32)
	m := &store.Message{ID: id, Version: 1, State: store.StateAvailable, CreatedAt: created, ExpiresAt: created.Add(time.Hour), TTLSeconds: 3600, OpensMax: 1, OpensRemaining: 1, PepperID: 7, ReadMAC: mac, ManageMAC: mac, VerifierMAC: mac, KDF: 2, Size: 64, BlobRef: id}
	if err := blobs.Put(ctx, id, bytes.NewReader(make([]byte, 64)), 64); err != nil {
		t.Fatal(err)
	}
	if err := st.Create(ctx, m); err != nil {
		t.Fatal(err)
	}
	claimed := created.Add(time.Minute)
	res, err := st.Claim(ctx, store.ClaimParams{ID: id, Clock: func() time.Time { return claimed }, VerifierSent: true, VerifyMAC: func(byte, []byte) bool { return true }, LeaseMAC: mac, LeasePepperID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AckLease(ctx, st, blobs, res.Lease, claimed); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SweepAll(ctx, st, blobs, created.Add(9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the tombstone was not purged: %v", err)
	}
	for _, name := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(name)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(id)) {
			t.Fatalf("%s still holds the id of a purged message", filepath.Base(name))
		}
	}
}

func TestOrderedTablesKeepNoInsertionOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	assertNoRowid(t, path)
}

func TestOpenRebuildsRowidTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE tombstones (id TEXT PRIMARY KEY, pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, state TEXT NOT NULL, first_opened_at INTEGER, ended_at INTEGER NOT NULL, purge_at INTEGER NOT NULL);
		CREATE TABLE passphrase_attempts (id TEXT NOT NULL, bucket BLOB NOT NULL, fail_count INTEGER NOT NULL, next_attempt_at INTEGER NOT NULL, purge_at INTEGER NOT NULL, PRIMARY KEY (id, bucket));
		CREATE TABLE rate_counters (key TEXT NOT NULL, window_start INTEGER NOT NULL, count INTEGER NOT NULL, bytes INTEGER NOT NULL, purge_at INTEGER NOT NULL, PRIMARY KEY (key, window_start));
		INSERT INTO tombstones VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 7, x'00', x'00', 'deleted', NULL, 1800000000000, 1800604800000);
		INSERT INTO passphrase_attempts VALUES ('AAAAAAAAAAAAAAAAAAAAAA', x'010203', 2, 0, 1800604800000);
		INSERT INTO rate_counters VALUES ('k', 0, 3, 0, 1800604800000);`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.Get(t.Context(), "AAAAAAAAAAAAAAAAAAAAAA")
	if err != nil || rec.Tombstone == nil || rec.Tombstone.State != store.StateDeleted {
		t.Fatalf("tombstone lost in the rebuild: %+v %v", rec, err)
	}
	if c, err := s.RateGet(t.Context(), "k", time.UnixMilli(0)); err != nil || c.Count != 3 {
		t.Fatalf("counter lost in the rebuild: %+v %v", c, err)
	}
	s.Close()
	assertNoRowid(t, path)
}

func assertNoRowid(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"tombstones", "rate_counters"} {
		if _, err := db.Exec(`SELECT rowid FROM ` + table); err == nil {
			t.Fatalf("%s keeps an insertion-order rowid", table)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'passphrase_attempts' OR sql LIKE '%day_fail%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("per-address attempt rows survive: %d %v", n, err)
	}
}

func TestPassphraseAttemptTimesAreRounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	id := core.NewID().String()
	created := time.Date(2026, 10, 22, 12, 34, 56, 789e6, time.UTC)
	mac := bytes.Repeat([]byte{7}, 32)
	if err := s.Create(ctx, &store.Message{ID: id, Version: 1, KDF: 2, State: store.StateAvailable, CreatedAt: created, ExpiresAt: created.Add(time.Hour), TTLSeconds: 3600, OpensMax: 1, OpensRemaining: 1, PepperID: 7, ReadMAC: mac, ManageMAC: mac, VerifierMAC: mac, Size: 64, BlobRef: id}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := created.Add(17*time.Second + 123*time.Millisecond)
	for i := 1; i <= store.FreeAttempts+1; i++ {
		_, err := s.Claim(ctx, store.ClaimParams{ID: id, Clock: func() time.Time { return at }, VerifierSent: true, VerifyMAC: func(byte, []byte) bool { return false }, LeaseMAC: mac, LeasePepperID: 7})
		if _, ok := errors.AsType[*store.PassphraseError](err); !ok {
			t.Fatalf("attempt %d: %v", i, err)
		}
		var next int64
		if err := db.QueryRow(`SELECT next_attempt_at FROM messages WHERE id = ?`, id).Scan(&next); err != nil {
			t.Fatal(err)
		}
		if i <= store.FreeAttempts && next != 0 || i > store.FreeAttempts && (next%time.Minute.Milliseconds() != 0 || next <= at.UnixMilli()) {
			t.Fatalf("attempt %d stored next_attempt_at %d, which reveals the attempt time", i, next)
		}
	}
}

func TestOpenRebuildsMessagesAndDropsAddressCounters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	old, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	const id = "AAAAAAAAAAAAAAAAAAAAAA"
	if _, err := old.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, version INTEGER NOT NULL, kdf_id INTEGER NOT NULL, flags INTEGER NOT NULL, state TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ttl_s INTEGER NOT NULL, opens_max INTEGER NOT NULL, opens_remaining INTEGER NOT NULL, pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, verifier_mac BLOB, size INTEGER NOT NULL, blob_ref TEXT, fail_count_total INTEGER NOT NULL DEFAULT 0, next_attempt_at INTEGER NOT NULL DEFAULT 0, first_opened_at INTEGER, ending TEXT);
		CREATE TABLE leases (row_id INTEGER PRIMARY KEY, id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE, lease_mac BLOB NOT NULL, pepper_id INTEGER NOT NULL, lease_expires_at INTEGER NOT NULL, downloads INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE rate_counters (key TEXT NOT NULL, window_start INTEGER NOT NULL, count INTEGER NOT NULL, bytes INTEGER NOT NULL, purge_at INTEGER NOT NULL, PRIMARY KEY (key, window_start)) WITHOUT ROWID;
		INSERT INTO messages VALUES ('` + id + `', 1, 0, 0, 'available', 1800000000000, 4102444800000, 3600, 2, 1, 7, x'00', x'00', NULL, 64, '` + id + `', 0, 0, 1800000000000, NULL);
		INSERT INTO leases VALUES (1, '` + id + `', x'0102', 7, 4102444800000, 0);
		INSERT INTO rate_counters VALUES ('create:h:0a1b2c', 0, 1, 0, 4102444800000), ('create:d:c:0a1b2c', 0, 1, 4096, 4102444800000), ('read:h:ffffff', 0, 1, 0, 4102444800000), ('abuse:h:c:123456', 0, 1, 0, 4102444800000),
			('abuse:id:` + id + `', 0, 1, 0, 4102444800000), ('global:d', 0, 5, 0, 4102444800000), ('challenge:00112233445566778899aabbccddeeff', 0, 1, 0, 4102444800000), ('create:h:abc', 0, 1, 0, 4102444800000),
			('abuse:id:` + id + `:0a1b2c', 0, 1, 0, 4102444800000), ('pow:00ff', 1800000001000, 1, 0, 4102444800000), ('pow:11ee', 0, 1, 0, 4102444800000);
		PRAGMA user_version = 3;`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.Get(t.Context(), id)
	if err != nil || rec.Message == nil || rec.Message.OpensRemaining != 1 {
		t.Fatalf("message lost in the rebuild: %+v %v", rec, err)
	}
	if leases, err := s.Leases(t.Context(), id); err != nil || len(leases) != 1 {
		t.Fatalf("the rebuild dropped the lease through its foreign key: %v %v", leases, err)
	}
	s.Close()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`SELECT rowid FROM messages`); err == nil {
		t.Fatal("messages keeps an insertion-order rowid")
	}
	rows, err := db.Query(`SELECT key FROM rate_counters ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var kept []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		kept = append(kept, k)
	}
	want := []string{"abuse:id:" + id, "challenge:00112233445566778899aabbccddeeff", "global:d", "pow:11ee"}
	if strings.Join(kept, ",") != strings.Join(want, ",") {
		t.Fatalf("counters after the upgrade: %v, want %v", kept, want)
	}
}

func TestLeaseRowIDsAreRandom(t *testing.T) {
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	id := core.NewID().String()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	mac := bytes.Repeat([]byte{7}, 32)
	if err := s.Create(ctx, &store.Message{ID: id, Version: 1, State: store.StateAvailable, CreatedAt: now, ExpiresAt: now.Add(time.Hour), TTLSeconds: 3600, OpensMax: 3, OpensRemaining: 3, PepperID: 7, ReadMAC: mac, ManageMAC: mac, Size: 64, BlobRef: id}); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		res, err := s.Claim(ctx, store.ClaimParams{ID: id, Clock: func() time.Time { return now }, LeaseMAC: []byte{byte(i)}, LeasePepperID: 7})
		if err != nil {
			t.Fatal(err)
		}
		if res.Lease.RowID < 1<<32 {
			t.Fatalf("lease row id %d looks sequential", res.Lease.RowID)
		}
	}
}

func TestSweepReportsABlockedCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nylzo.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RateHit(t.Context(), []store.RateHit{{Key: "k", WindowStart: time.UnixMilli(0), PurgeAt: time.UnixMilli(0)}}); err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM rate_counters`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SweepAll(t.Context(), s, blobs, time.Now()); !errors.Is(err, sqlite.ErrCheckpointBusy) {
		t.Fatalf("sweep with a reader holding the WAL: %v", err)
	}
	tx.Rollback()
	if _, err := store.SweepAll(t.Context(), s, blobs, time.Now()); err != nil {
		t.Fatalf("sweep after the reader left: %v", err)
	}
}

func TestOpenRefusesANewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	newer, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newer.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatal(err)
	}
	newer.Close()
	if s, err := sqlite.Open(path); !errors.Is(err, sqlite.ErrNewerSchema) {
		if s != nil {
			s.Close()
		}
		t.Fatalf("open a database from a newer build: %v", err)
	}
	check, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var tables int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("the refused open still changed the schema: %d %v", tables, err)
	}
}

func TestOlderBuildWritesFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	id := core.NewID().String()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	mac := bytes.Repeat([]byte{7}, 32)
	if err := s.Create(ctx, &store.Message{ID: id, Version: 1, State: store.StateAvailable, CreatedAt: now, ExpiresAt: now.Add(time.Hour), TTLSeconds: 3600, OpensMax: 3, OpensRemaining: 3, PepperID: 7, ReadMAC: mac, ManageMAC: mac, Size: 64, BlobRef: id}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Claim(ctx, store.ClaimParams{ID: id, Clock: func() time.Time { return now }, LeaseMAC: mac, LeasePepperID: 7})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	const counter = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES (?, ?, 1, 0, 4102444800000)
		ON CONFLICT(key, window_start) DO UPDATE SET count = count + 1 RETURNING count`
	day := now.Truncate(24 * time.Hour).UnixMilli()
	for _, refused := range []struct {
		key    string
		window int64
	}{
		{"create:h:0a1b2c", day}, {"create:d:c:0a1b2c", day}, {"read:h:ffffff", day}, {"abuse:h:c:123456", day},
		{"abuse:id:" + id + ":0a1b2c", day}, {"pow:00112233445566778899aabbccddeeff", now.Add(17 * time.Second).UnixMilli()},
	} {
		var n int
		if err := old.QueryRow(counter, refused.key, refused.window).Scan(&n); err == nil {
			t.Errorf("an older build stored the counter %s", refused.key)
		}
	}
	for _, allowed := range []string{"global:d", "abuse:id:" + id, "challenge:00112233445566778899aabbccddeeff"} {
		var n int
		if err := old.QueryRow(counter, allowed, 0).Scan(&n); err != nil {
			t.Errorf("the guard refused %s: %v", allowed, err)
		}
	}
	if _, err := old.Exec(`UPDATE rate_counters SET key = 'create:h:0a1b2c' WHERE key GLOB 'challenge:*'`); err == nil {
		t.Error("an older build renamed a counter to an address key")
	}
	for name, stmt := range map[string]string{
		"lease without row id":    `INSERT INTO leases (id, lease_mac, pepper_id, lease_expires_at, downloads) VALUES ('` + id + `', x'01', 7, 4102444800000, 0)`,
		"lease with a low row id": `INSERT INTO leases (row_id, id, lease_mac, pepper_id, lease_expires_at, downloads) VALUES (5, '` + id + `', x'01', 7, 4102444800000, 0)`,
		"lease moved to a low id": fmt.Sprintf(`UPDATE leases SET row_id = 5 WHERE row_id = %d`, res.Lease.RowID),
	} {
		if _, err := old.Exec(stmt); err == nil {
			t.Errorf("an older build wrote a %s", name)
		}
	}
	if _, err := old.Exec(fmt.Sprintf(`UPDATE leases SET downloads = 1 WHERE row_id = %d`, res.Lease.RowID)); err != nil {
		t.Errorf("the guard refused a download count: %v", err)
	}
}

func TestWALHoldsOnlyTheLastTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	mac := bytes.Repeat([]byte{7}, 32)
	for range 5 {
		id := core.NewID().String()
		if err := s.Create(ctx, &store.Message{ID: id, Version: 1, State: store.StateAvailable, CreatedAt: now, ExpiresAt: now.Add(time.Hour), TTLSeconds: 3600, OpensMax: 1, OpensRemaining: 1, PepperID: 7, ReadMAC: mac, ManageMAC: mac, Size: 64, BlobRef: id}); err != nil {
			t.Fatal(err)
		}
	}
	wal, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if commits := walCommits(t, wal); commits > 1 {
		t.Fatalf("the WAL keeps %d transactions, so it shows the order of earlier writes", commits)
	}
}

func walCommits(t *testing.T, wal []byte) int {
	t.Helper()
	const header, frameHeader = 32, 24
	if len(wal) < header {
		return 0
	}
	pageSize := int(binary.BigEndian.Uint32(wal[8:12]))
	salts := wal[16:24]
	commits := 0
	for off := header; off+frameHeader+pageSize <= len(wal); off += frameHeader + pageSize {
		frame := wal[off : off+frameHeader]
		if bytes.Equal(frame[8:16], salts) && binary.BigEndian.Uint32(frame[4:8]) != 0 {
			commits++
		}
	}
	return commits
}

type cancelAfterAck struct {
	store.Store
	cancel context.CancelFunc
}

func (c cancelAfterAck) Ack(ctx context.Context, l store.Lease, now time.Time) (string, error) {
	ref, err := c.Store.Ack(context.WithoutCancel(ctx), l, now)
	c.cancel()
	return ref, err
}

func TestAnAckFinishesItsEndingAfterTheClientLeft(t *testing.T) {
	dir := t.TempDir()
	s, err := sqlite.Open(filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	id := core.NewID().String()
	mac := bytes.Repeat([]byte{7}, 32)
	if err := blobs.Put(t.Context(), id, bytes.NewReader(make([]byte, 64)), 64); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(t.Context(), &store.Message{ID: id, Version: 1, State: store.StateAvailable, CreatedAt: now, ExpiresAt: now.Add(time.Hour), TTLSeconds: 3600, OpensMax: 1, OpensRemaining: 1, PepperID: 7, ReadMAC: mac, ManageMAC: mac, Size: 64, BlobRef: id}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Claim(t.Context(), store.ClaimParams{ID: id, Clock: func() time.Time { return now }, LeaseMAC: mac, LeasePepperID: 7})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := store.AckLease(ctx, cancelAfterAck{s, cancel}, blobs, res.Lease, now); err != nil {
		t.Fatalf("an ack whose client left after the commit: %v", err)
	}
	if rec, err := s.Get(t.Context(), id); err != nil || rec.Tombstone == nil {
		t.Fatalf("the ack left the message without its tombstone: %+v %v", rec, err)
	}
}
