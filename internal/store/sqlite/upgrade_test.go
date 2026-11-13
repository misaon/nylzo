package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

type released struct {
	path   string
	marker []byte
}

// releasedDatabase has the tables and version of a release, the guards of nylzo 0.2.3 for schema 5, and one row whose
// bytes nothing else holds.
func releasedDatabase(t *testing.T, version int) released {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	database, err := sql.Open("sqlite", dataSourceName(path, true))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	marker := bytes.Repeat([]byte("released-row"), 8)
	_, err = database.Exec(`CREATE TABLE messages (
			locator BLOB PRIMARY KEY, read_wrap BLOB NOT NULL, manage_wrap BLOB NOT NULL, record BLOB NOT NULL,
			blob_ref TEXT, blob_wrap BLOB, blob_purge_at INTEGER, purge_at INTEGER NOT NULL
		) WITHOUT ROWID;
		CREATE TABLE rate_counters (
			key TEXT NOT NULL, window_start INTEGER NOT NULL, count INTEGER NOT NULL, bytes INTEGER NOT NULL,
			purge_at INTEGER NOT NULL, PRIMARY KEY (key, window_start)
		) WITHOUT ROWID;
		INSERT INTO rate_counters VALUES ('global:day', 0, 3, 0, 0);
		PRAGMA user_version = ` + strconv.Itoa(version))
	if err == nil && version == 5 {
		_, err = database.Exec(`CREATE TRIGGER rate_counters_guard_insert BEFORE INSERT ON rate_counters
			WHEN NEW.key GLOB 'create:*' BEGIN SELECT RAISE(ABORT, 'older build'); END`)
	}
	if err == nil {
		_, err = database.Exec(
			`INSERT INTO messages VALUES (?, ?, ?, ?, 'ref', NULL, NULL, 0)`,
			marker[:32], marker, marker, marker,
		)
	}
	if err != nil {
		t.Fatal(err)
	}
	return released{path: path, marker: marker}
}

var errInterrupted = errors.New("interrupted")

func TestUpgradeFromAReleaseFinishesAfterAnInterruptionAtEveryStep(t *testing.T) {
	t.Parallel()
	for name, interrupt := range map[string]func(t *testing.T, path string){
		"while erasing the blobs": func(t *testing.T, path string) {
			t.Helper()
			if _, err := UpgradeFromReleased(t.Context(), path, func() error { return errInterrupted }); !errors.Is(err, errInterrupted) {
				t.Fatalf("an interrupted blob erase: %v", err)
			}
		},
		"after deleting the rows": func(t *testing.T, path string) {
			t.Helper()
			database, err := openPersistent(path, true)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err = deleteReleasedMessages(t.Context(), database); err != nil {
				t.Fatal(err)
			}
		},
		"never": func(*testing.T, string) {},
	} {
		for _, version := range releasedSchemas {
			upgradeAfterInterruption(t, name+", schema "+strconv.Itoa(version), version, interrupt)
		}
	}
}

func upgradeAfterInterruption(t *testing.T, name string, version int, interrupt func(t *testing.T, path string)) {
	t.Helper()
	{
		database := releasedDatabase(t, version)
		path, marker := database.path, database.marker
		interrupt(t, path)
		erased := 0
		upgraded, err := UpgradeFromReleased(t.Context(), path, func() error {
			erased++
			return nil
		})
		if err != nil || !upgraded || erased != 1 {
			t.Fatalf("%s: upgraded %t, blobs erased %d times, %v", name, upgraded, erased, err)
		}
		opened, err := Open(t.Context(), path)
		if err != nil {
			t.Fatalf("%s: the upgraded database does not open: %v", name, err)
		}
		var rows, counters int
		err = errors.Join(
			opened.reader.QueryRow(`SELECT COUNT(*) FROM messages WHERE locator != ?`, store.DummyLocator[:]).
				Scan(&rows),
			opened.reader.QueryRow(`SELECT COUNT(*) FROM rate_counters`).Scan(&counters),
			opened.Close(),
		)
		if err != nil || rows != 0 || counters != 0 {
			t.Fatalf("%s: %d rows and %d counters left, %v", name, rows, counters, err)
		}
		for _, file := range []string{path, path + "-wal"} {
			content, readErr := os.ReadFile(file)
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal(readErr)
			}
			if bytes.Contains(content, marker[:24]) {
				t.Fatalf("%s: %s still holds a row from before the upgrade", name, filepath.Base(file))
			}
		}
		if again, againErr := UpgradeFromReleased(t.Context(), path, func() error { return errInterrupted }); again ||
			againErr != nil {
			t.Fatalf("%s: a second start upgraded again: %t %v", name, again, againErr)
		}
	}
}

// TestOpenAddsTheCounterPurgeIndexAndKeepsEveryRow covers the counter purge index: the purge reads an index on
// purge_at, so a flood of challenge rows costs no scan of the table per batch, and a database of the current schema
// without the index keeps its rows and its schema version.
func TestOpenAddsTheCounterPurgeIndexAndKeepsEveryRow(t *testing.T) {
	t.Parallel()
	for name, open := range map[string]func(context.Context, string) (*Store, error){
		"serve":      Open,
		"sweep-once": OpenCurrent,
	} {
		path := filepath.Join(t.TempDir(), "nylzo.db")
		storage, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		hit := store.RateHit{Key: "global:stored:d", PurgeAt: time.UnixMilli(0)}
		if _, err = storage.RateHit(t.Context(), []store.RateHit{hit}); err != nil {
			t.Fatal(err)
		}
		_, err = storage.writer.Exec(`DROP INDEX IF EXISTS rate_counters_purge_at`)
		if err = errors.Join(err, storage.Close()); err != nil {
			t.Fatal(err)
		}
		if readiness, inspectErr := Inspect(t.Context(), path); inspectErr != nil || readiness != ReadinessOpen {
			t.Fatalf("%s: store-check without the index: %v %v", name, readiness, inspectErr)
		}
		opened, err := open(t.Context(), path)
		if err != nil {
			t.Fatalf("%s: open without the index: %v", name, err)
		}
		version, err := userVersion(t.Context(), opened.reader)
		count, countErr := opened.RateGet(t.Context(), hit.Key, hit.WindowStart)
		plan, planErr := queryPlan(opened.reader, purgeQuery, 0, store.CounterPurgeBatch)
		if err = errors.Join(err, countErr, planErr, opened.Close()); err != nil || version != schemaVersion ||
			count.Count != 1 {
			t.Fatalf("%s: version %d, counter %+v, %v", name, version, count, err)
		}
		if !strings.Contains(plan, "rate_counters_purge_at") {
			t.Fatalf("%s: the counter purge scans the table:\n%s", name, plan)
		}
	}
}

func queryPlan(database *sql.DB, query string, arguments ...any) (string, error) {
	rows, err := database.Query(`EXPLAIN QUERY PLAN `+query, arguments...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if scanErr := rows.Scan(&id, &parent, &unused, &detail); scanErr != nil {
			return "", scanErr
		}
		plan.WriteString(detail + "\n")
	}
	return plan.String(), rows.Err()
}
