package sqlite

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/misaon/nylzo/internal/store"
)

type released struct {
	path   string
	marker []byte
}

// releasedDatabase has the tables, guards and version of nylzo 0.2.3 and one row whose bytes nothing else holds.
func releasedDatabase(t *testing.T) released {
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
		CREATE TRIGGER rate_counters_guard_insert BEFORE INSERT ON rate_counters WHEN NEW.key GLOB 'create:*'
			BEGIN SELECT RAISE(ABORT, 'older build'); END;
		INSERT INTO rate_counters VALUES ('global:day', 0, 3, 0, 0);
		PRAGMA user_version = 5;`)
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

func TestUpgradeFromAReleaseFinishesAfterAnInterruptionAtEveryStep(t *testing.T) {
	t.Parallel()
	interrupted := errors.New("interrupted")
	for name, interrupt := range map[string]func(t *testing.T, path string){
		"while erasing the blobs": func(t *testing.T, path string) {
			t.Helper()
			if _, err := UpgradeFromReleased(t.Context(), path, func() error { return interrupted }); !errors.Is(err, interrupted) {
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
		database := releasedDatabase(t)
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
		if again, againErr := UpgradeFromReleased(t.Context(), path, func() error { return interrupted }); again ||
			againErr != nil {
			t.Fatalf("%s: a second start upgraded again: %t %v", name, again, againErr)
		}
	}
}
