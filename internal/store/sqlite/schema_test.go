package sqlite_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store/sqlite"
)

const legacyID = "AAAAAAAAAAAAAAAAAAAAAA"

const version4Tables = `CREATE TABLE messages (id TEXT PRIMARY KEY, version INTEGER NOT NULL, kdf_id INTEGER NOT NULL, flags INTEGER NOT NULL, state TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ttl_s INTEGER NOT NULL, opens_max INTEGER NOT NULL, opens_remaining INTEGER NOT NULL, pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, verifier_mac BLOB, size INTEGER NOT NULL, blob_ref TEXT, fail_count_total INTEGER NOT NULL DEFAULT 0, next_attempt_at INTEGER NOT NULL DEFAULT 0, first_opened_at INTEGER, ending TEXT) WITHOUT ROWID;
	CREATE TABLE leases (row_id INTEGER PRIMARY KEY, id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE, lease_mac BLOB NOT NULL, pepper_id INTEGER NOT NULL, lease_expires_at INTEGER NOT NULL, downloads INTEGER NOT NULL DEFAULT 0);
	CREATE TABLE tombstones (id TEXT PRIMARY KEY, pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, state TEXT NOT NULL, first_opened_at INTEGER, ended_at INTEGER NOT NULL, purge_at INTEGER NOT NULL) WITHOUT ROWID;
	CREATE TABLE rate_counters (key TEXT NOT NULL, window_start INTEGER NOT NULL, count INTEGER NOT NULL, bytes INTEGER NOT NULL, purge_at INTEGER NOT NULL, PRIMARY KEY (key, window_start)) WITHOUT ROWID;
	INSERT INTO messages VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 1, 0, 0, 'available', 1800000000000, 4102444800000, 3600, 2, 1, 7, x'00', x'00', NULL, 64, 'AAAAAAAAAAAAAAAAAAAAAA', 0, 0, 1800000000000, NULL);
	INSERT INTO leases VALUES (4294967296, 'AAAAAAAAAAAAAAAAAAAAAA', x'0102', 7, 4102444800000, 0);
	INSERT INTO tombstones VALUES ('AAAAAAAAAAAAAAAAAAAAAQ', 7, x'00', x'00', 'deleted', NULL, 1800000000000, 1800604800000);
	INSERT INTO rate_counters VALUES ('abuse:id:AAAAAAAAAAAAAAAAAAAAAA', 0, 1, 0, 4102444800000), ('global:d', 0, 5, 0, 4102444800000), ('challenge:00112233445566778899aabbccddeeff', 0, 1, 0, 4102444800000), ('create:h:abc', 0, 1, 0, 4102444800000), ('pow:11ee', 0, 1, 0, 4102444800000);
	PRAGMA user_version = 4;`

const rowidCounters = `CREATE TABLE rate_counters (key TEXT NOT NULL, window_start INTEGER NOT NULL, count INTEGER NOT NULL, bytes INTEGER NOT NULL, purge_at INTEGER NOT NULL, PRIMARY KEY (key, window_start));
	INSERT INTO rate_counters VALUES ('k', 0, 3, 0, 1800604800000);`

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

// TestOpenDropsTheMessagesItCannotSeal covers D101: rows of version 4 lie in the clear and no key exists to seal them.
func TestOpenDropsTheMessagesItCannotSeal(t *testing.T) {
	t.Parallel()
	path := olderDatabase(t, version4Tables)
	if err := openConcurrently(t.Context(), path, 4); err != nil {
		t.Fatal(err)
	}
	tables := columnStrings(t, path, `SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if !slices.Equal(tables, []string{"messages", "rate_counters"}) {
		t.Fatalf("tables after the upgrade: %v", tables)
	}
	columns := columnStrings(t, path, `SELECT name FROM pragma_table_info('messages') ORDER BY cid`)
	want := []string{
		"locator",
		"read_wrap",
		"manage_wrap",
		"record",
		"blob_ref",
		"blob_wrap",
		"blob_purge_at",
		"purge_at",
	}
	if !slices.Equal(columns, want) {
		t.Fatalf("message columns after the upgrade: %v", columns)
	}
	if rows := columnStrings(t, path, `SELECT hex(locator) FROM messages`); len(rows) != 0 {
		t.Fatalf("unsealed messages survived the upgrade: %v", rows)
	}
	kept := columnStrings(t, path, `SELECT key FROM rate_counters ORDER BY key`)
	if !slices.Equal(kept, []string{"challenge:00112233445566778899aabbccddeeff", "global:d", "pow:11ee"}) {
		t.Fatalf("counters after the upgrade: %v", kept)
	}
	if version := columnStrings(t, path, `PRAGMA user_version`); !slices.Equal(version, []string{"5"}) {
		t.Fatalf("user_version after the upgrade: %v", version)
	}
}

func TestTablesKeepNoInsertionOrder(t *testing.T) {
	t.Parallel()
	fresh := filepath.Join(t.TempDir(), "nylzo.db")
	openStore(t, fresh).Close()
	rebuilt := olderDatabase(t, rowidCounters)
	storage := openStore(t, rebuilt)
	if count, err := storage.RateGet(t.Context(), "k", time.UnixMilli(0)); err != nil || count.Count != 3 {
		t.Fatalf("the counter was lost in the rebuild: %+v %v", count, err)
	}
	storage.Close()
	for _, path := range []string{fresh, rebuilt} {
		database := openRaw(t, path)
		for _, table := range []string{"messages", "rate_counters"} {
			if _, err := database.Exec(`SELECT rowid FROM ` + table); err == nil {
				t.Fatalf("%s keeps an insertion-order rowid", table)
			}
		}
	}
}

func TestOpenRefusesANewerSchema(t *testing.T) {
	t.Parallel()
	path := olderDatabase(t, `PRAGMA user_version = 6`)
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
	openStore(t, path).Close()
	older := openRaw(t, path)
	const counter = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES (?, ?, 1, 0, 4102444800000)
		ON CONFLICT(key, window_start) DO UPDATE SET count = count + 1 RETURNING count`
	day := testNow.Truncate(24 * time.Hour).UnixMilli()
	for key, window := range map[string]int64{
		"create:h:0a1b2c":                      day,
		"create:d:c:0a1b2c":                    day,
		"read:h:ffffff":                        day,
		"abuse:h:c:123456":                     day,
		"abuse:id:" + legacyID + ":0a1b2c":     day,
		"pow:00112233445566778899aabbccddeeff": testNow.Add(17 * time.Second).UnixMilli(),
	} {
		var count int
		if err := older.QueryRow(counter, key, window).Scan(&count); err == nil {
			t.Errorf("an older build stored the counter %s", key)
		}
	}
	for _, key := range []string{"global:d", "challenge:00112233445566778899aabbccddeeff"} {
		var count int
		if err := older.QueryRow(counter, key, 0).Scan(&count); err != nil {
			t.Errorf("the guard refused %s: %v", key, err)
		}
	}
	renamed := `UPDATE rate_counters SET key = 'create:h:0a1b2c' WHERE key GLOB 'challenge:*'`
	if _, err := older.Exec(renamed); err == nil {
		t.Error("an older build renamed a counter to an address key")
	}
}
