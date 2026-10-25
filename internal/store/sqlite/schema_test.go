package sqlite_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const olderMessages = `CREATE TABLE messages (id TEXT PRIMARY KEY, version INTEGER NOT NULL, kdf_id INTEGER NOT NULL, flags INTEGER NOT NULL, state TEXT NOT NULL,
	created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ttl_s INTEGER NOT NULL, opens_max INTEGER NOT NULL, opens_remaining INTEGER NOT NULL,
	pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, verifier_mac BLOB, size INTEGER NOT NULL, blob_ref TEXT,
	day_fail_count INTEGER NOT NULL DEFAULT 0, day_fail_date TEXT NOT NULL DEFAULT '', fail_count_total INTEGER NOT NULL DEFAULT 0, first_opened_at INTEGER%s);
	%s;
	INSERT INTO messages (id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, size, blob_ref)
	VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 1, 0, 0, 'available', 0, 9999999999999, 60, 1, 1, 7, x'00', x'00', 64, 'AAAAAAAAAAAAAAAAAAAAAA');`

const rowidTables = `CREATE TABLE tombstones (id TEXT PRIMARY KEY, pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, state TEXT NOT NULL, first_opened_at INTEGER, ended_at INTEGER NOT NULL, purge_at INTEGER NOT NULL);
	CREATE TABLE passphrase_attempts (id TEXT NOT NULL, bucket BLOB NOT NULL, fail_count INTEGER NOT NULL, next_attempt_at INTEGER NOT NULL, purge_at INTEGER NOT NULL, PRIMARY KEY (id, bucket));
	CREATE TABLE rate_counters (key TEXT NOT NULL, window_start INTEGER NOT NULL, count INTEGER NOT NULL, bytes INTEGER NOT NULL, purge_at INTEGER NOT NULL, PRIMARY KEY (key, window_start));
	INSERT INTO tombstones VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 7, x'00', x'00', 'deleted', NULL, 1800000000000, 1800604800000);
	INSERT INTO passphrase_attempts VALUES ('AAAAAAAAAAAAAAAAAAAAAA', x'010203', 2, 0, 1800604800000);
	INSERT INTO rate_counters VALUES ('k', 0, 3, 0, 1800604800000);`

const version3Tables = `CREATE TABLE messages (id TEXT PRIMARY KEY, version INTEGER NOT NULL, kdf_id INTEGER NOT NULL, flags INTEGER NOT NULL, state TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ttl_s INTEGER NOT NULL, opens_max INTEGER NOT NULL, opens_remaining INTEGER NOT NULL, pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, verifier_mac BLOB, size INTEGER NOT NULL, blob_ref TEXT, fail_count_total INTEGER NOT NULL DEFAULT 0, next_attempt_at INTEGER NOT NULL DEFAULT 0, first_opened_at INTEGER, ending TEXT);
	CREATE TABLE leases (row_id INTEGER PRIMARY KEY, id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE, lease_mac BLOB NOT NULL, pepper_id INTEGER NOT NULL, lease_expires_at INTEGER NOT NULL, downloads INTEGER NOT NULL DEFAULT 0);
	CREATE TABLE rate_counters (key TEXT NOT NULL, window_start INTEGER NOT NULL, count INTEGER NOT NULL, bytes INTEGER NOT NULL, purge_at INTEGER NOT NULL, PRIMARY KEY (key, window_start)) WITHOUT ROWID;
	INSERT INTO messages VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 1, 0, 0, 'available', 1800000000000, 4102444800000, 3600, 2, 1, 7, x'00', x'00', NULL, 64, 'AAAAAAAAAAAAAAAAAAAAAA', 0, 0, 1800000000000, NULL);
	INSERT INTO leases VALUES (1, 'AAAAAAAAAAAAAAAAAAAAAA', x'0102', 7, 4102444800000, 0);
	INSERT INTO rate_counters VALUES ('create:h:0a1b2c', 0, 1, 0, 4102444800000), ('create:d:c:0a1b2c', 0, 1, 4096, 4102444800000), ('read:h:ffffff', 0, 1, 0, 4102444800000), ('abuse:h:c:123456', 0, 1, 0, 4102444800000),
		('abuse:id:AAAAAAAAAAAAAAAAAAAAAA', 0, 1, 0, 4102444800000), ('global:d', 0, 5, 0, 4102444800000), ('challenge:00112233445566778899aabbccddeeff', 0, 1, 0, 4102444800000), ('create:h:abc', 0, 1, 0, 4102444800000),
		('abuse:id:AAAAAAAAAAAAAAAAAAAAAA:0a1b2c', 0, 1, 0, 4102444800000), ('pow:00ff', 1800000001000, 1, 0, 4102444800000), ('pow:11ee', 0, 1, 0, 4102444800000);
	PRAGMA user_version = 3;`

func olderDatabase(t *testing.T, statements string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	if _, err := openRaw(t, path).Exec(statements); err != nil {
		t.Fatal(err)
	}
	return path
}

func columnStrings(t *testing.T, path, query string) []string {
	t.Helper()
	rows, err := openRaw(t, path).Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func TestOpenMigratesOlderSchema(t *testing.T) {
	t.Parallel()
	for name, statements := range map[string]string{
		"without ending": fmt.Sprintf(olderMessages, "", "CREATE INDEX messages_release ON messages(opens_remaining, blob_ref)"),
		"with release_id": fmt.Sprintf(
			olderMessages,
			", ending TEXT",
			"CREATE INDEX messages_release_id ON messages(opens_remaining, id) WHERE blob_ref IS NOT NULL",
		),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := olderDatabase(t, statements)
			if err := openConcurrently(t.Context(), path, 4); err != nil {
				t.Fatal(err)
			}
			storage := openStore(t, path)
			state, ref, _, err := storage.MarkEnding(t.Context(), legacyID, store.StateCancelled, time.Unix(0, 0))
			if err != nil || state != store.StateCancelled || ref == "" {
				t.Fatalf("mark ending after the migration: %s %q %v", state, ref, err)
			}
			record, err := storage.Get(t.Context(), legacyID)
			if err != nil || record.Message == nil || record.Message.Ending != store.StateCancelled {
				t.Fatalf("get after the migration: %+v %v", record, err)
			}
			indexes := columnStrings(t, path, `SELECT name FROM sqlite_master WHERE type = 'index'
				AND tbl_name = 'messages' AND sql IS NOT NULL ORDER BY name`)
			if !slices.Equal(indexes, []string{"messages_ending", "messages_expires_at", "messages_releasable"}) {
				t.Fatalf("message indexes after the migration: %v", indexes)
			}
		})
	}
}

func TestTablesKeepNoInsertionOrder(t *testing.T) {
	t.Parallel()
	fresh := filepath.Join(t.TempDir(), "nylzo.db")
	openStore(t, fresh).Close()
	rebuilt := olderDatabase(t, rowidTables)
	storage := openStore(t, rebuilt)
	record, err := storage.Get(t.Context(), legacyID)
	if err != nil || record.Tombstone == nil || record.Tombstone.State != store.StateDeleted {
		t.Fatalf("the tombstone was lost in the rebuild: %+v %v", record, err)
	}
	if count, countErr := storage.RateGet(t.Context(), "k", time.UnixMilli(0)); countErr != nil || count.Count != 3 {
		t.Fatalf("the counter was lost in the rebuild: %+v %v", count, countErr)
	}
	storage.Close()
	for _, path := range []string{fresh, rebuilt} {
		database := openRaw(t, path)
		for _, table := range []string{"messages", "tombstones", "rate_counters"} {
			if _, err = database.Exec(`SELECT rowid FROM ` + table); err == nil {
				t.Fatalf("%s keeps an insertion-order rowid", table)
			}
		}
		var perAddress int
		err = database.QueryRow(`SELECT COUNT(*) FROM sqlite_master
			WHERE name = 'passphrase_attempts' OR sql LIKE '%day_fail%'`).Scan(&perAddress)
		if err != nil || perAddress != 0 {
			t.Fatalf("per-address attempt rows survive: %d %v", perAddress, err)
		}
	}
}

func TestOpenRebuildsMessagesAndDropsAddressCounters(t *testing.T) {
	t.Parallel()
	path := olderDatabase(t, version3Tables)
	storage := openStore(t, path)
	record, err := storage.Get(t.Context(), legacyID)
	if err != nil || record.Message == nil || record.Message.OpensRemaining != 1 {
		t.Fatalf("the message was lost in the rebuild: %+v %v", record, err)
	}
	if leases, leasesErr := storage.Leases(t.Context(), legacyID); leasesErr != nil || len(leases) != 1 {
		t.Fatalf("the rebuild dropped the lease through its foreign key: %v %v", leases, leasesErr)
	}
	storage.Close()
	kept := columnStrings(t, path, `SELECT key FROM rate_counters ORDER BY key`)
	want := []string{
		"abuse:id:" + legacyID,
		"challenge:00112233445566778899aabbccddeeff",
		"global:d",
		"pow:11ee",
	}
	if !slices.Equal(kept, want) {
		t.Fatalf("counters after the upgrade: %v, want %v", kept, want)
	}
}

func TestLegacyTombstoneEndedAtIsRounded(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	openStore(t, path).Close()
	ended := time.Date(2026, 10, 22, 10, 1, 35, 300e6, time.UTC)
	_, err := openRaw(t, path).Exec(
		`INSERT INTO tombstones (id, pepper_id, read_mac, manage_mac, state, ended_at, purge_at)
			VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 7, x'00', x'00', 'deleted', ?, ?)`,
		ended.UnixMilli(),
		ended.Add(store.TombstoneTTL).UnixMilli(),
	)
	if err != nil {
		t.Fatal(err)
	}
	record, err := openStore(t, path).Get(t.Context(), legacyID)
	if err != nil || record.Tombstone == nil || !record.Tombstone.EndedAt.Equal(ended.Truncate(time.Minute)) {
		t.Fatalf("a tombstone written before the rounding must be served rounded: %+v %v", record, err)
	}
}

func TestOpenRefusesANewerSchema(t *testing.T) {
	t.Parallel()
	path := olderDatabase(t, `PRAGMA user_version = 5`)
	if opened, err := sqlite.Open(t.Context(), path); !errors.Is(err, sqlite.ErrNewerSchema) {
		if opened != nil {
			opened.Close()
		}
		t.Fatalf("open a database from a newer build: %v", err)
	}
	if tables := columnStrings(t, path, `SELECT name FROM sqlite_master`); len(tables) != 0 {
		t.Fatalf("the refused open still changed the schema: %v", tables)
	}
}

func TestOlderBuildWritesFail(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	message := newMessage(3, false)
	create(t, storage, message)
	lease := claim(t, storage, message, testNow).Lease
	storage.Close()
	older := openRaw(t, path)
	const counter = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES (?, ?, 1, 0, 4102444800000)
		ON CONFLICT(key, window_start) DO UPDATE SET count = count + 1 RETURNING count`
	day := testNow.Truncate(24 * time.Hour).UnixMilli()
	for key, window := range map[string]int64{
		"create:h:0a1b2c":                      day,
		"create:d:c:0a1b2c":                    day,
		"read:h:ffffff":                        day,
		"abuse:h:c:123456":                     day,
		"abuse:id:" + message.ID + ":0a1b2c":   day,
		"pow:00112233445566778899aabbccddeeff": testNow.Add(17 * time.Second).UnixMilli(),
	} {
		var count int
		if err := older.QueryRow(counter, key, window).Scan(&count); err == nil {
			t.Errorf("an older build stored the counter %s", key)
		}
	}
	for _, key := range []string{"global:d", "abuse:id:" + message.ID, "challenge:00112233445566778899aabbccddeeff"} {
		var count int
		if err := older.QueryRow(counter, key, 0).Scan(&count); err != nil {
			t.Errorf("the guard refused %s: %v", key, err)
		}
	}
	for name, statement := range map[string]string{
		"counter renamed to an address key": `UPDATE rate_counters SET key = 'create:h:0a1b2c' WHERE key GLOB 'challenge:*'`,
		"lease without row id": `INSERT INTO leases (id, lease_mac, pepper_id, lease_expires_at, downloads)
			VALUES ('` + message.ID + `', x'01', 7, 4102444800000, 0)`,
		"lease with a low row id": `INSERT INTO leases (row_id, id, lease_mac, pepper_id, lease_expires_at, downloads)
			VALUES (5, '` + message.ID + `', x'01', 7, 4102444800000, 0)`,
		"lease moved to a low id": fmt.Sprintf(`UPDATE leases SET row_id = 5 WHERE row_id = %d`, lease.RowID),
	} {
		if _, err := older.Exec(statement); err == nil {
			t.Errorf("an older build wrote a %s", name)
		}
	}
	if _, err := older.Exec(fmt.Sprintf(`UPDATE leases SET downloads = 1 WHERE row_id = %d`, lease.RowID)); err != nil {
		t.Errorf("the guard refused a download count: %v", err)
	}
}
