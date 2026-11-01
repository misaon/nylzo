package sqlite_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/misaon/nylzo/internal/store/sqlite"
)

func rawDatabase(t *testing.T, statements string) string {
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

func TestTablesKeepNoInsertionOrder(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	openStore(t, path).Close()
	database := openRaw(t, path)
	for _, table := range []string{"messages", "rate_counters"} {
		if _, err := database.Exec(`SELECT rowid FROM ` + table); err == nil {
			t.Fatalf("%s keeps an insertion-order rowid", table)
		}
	}
}

func TestOpenRefusesAnotherSchemaVersion(t *testing.T) {
	t.Parallel()
	const preRelease = `CREATE TABLE messages (id TEXT PRIMARY KEY, state TEXT NOT NULL) WITHOUT ROWID;
		INSERT INTO messages VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 'available');`
	for name, testCase := range map[string]struct {
		statements string
		want       error
	}{
		"a newer version":                    {`PRAGMA user_version = 7;`, sqlite.ErrNewerSchema},
		"a pre-release version":              {preRelease + `PRAGMA user_version = 5;`, sqlite.ErrOlderSchema},
		"pre-release tables without version": {preRelease, sqlite.ErrOlderSchema},
	} {
		path := rawDatabase(t, testCase.statements)
		before := schemaOf(t, path)
		if opened, err := sqlite.Open(t.Context(), path); !errors.Is(err, testCase.want) {
			if opened != nil {
				opened.Close()
			}
			t.Fatalf("%s: %v, want %v", name, err, testCase.want)
		}
		if after := schemaOf(t, path); !slices.Equal(after, before) {
			t.Fatalf("%s: the refused open changed the schema from %v to %v", name, before, after)
		}
	}
}

func schemaOf(t *testing.T, path string) []string {
	t.Helper()
	return slices.Concat(
		columnStrings(t, path, `PRAGMA user_version`),
		columnStrings(t, path, `SELECT name FROM sqlite_master ORDER BY name`),
	)
}
