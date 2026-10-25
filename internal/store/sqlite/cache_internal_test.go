package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestPageCacheIsBounded(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, db := range []*sql.DB{s.w, s.r} {
		var kib int
		if err := db.QueryRow("PRAGMA cache_size").Scan(&kib); err != nil {
			t.Fatal(err)
		}
		if kib >= 0 || -kib > maxCacheKiB {
			t.Fatalf("page cache %d, want at most %d KiB per connection outside the Go heap", kib, maxCacheKiB)
		}
		var temp int
		if err := db.QueryRow("PRAGMA temp_store").Scan(&temp); err != nil || temp != 2 {
			t.Fatalf(
				"temp_store %d %v: SQLite may write temporary copies of rows outside the data directory",
				temp,
				err,
			)
		}
	}
}

func TestMigrationStopsWhenItsContextEnds(t *testing.T) {
	db, err := sql.Open("sqlite", dsn(filepath.Join(t.TempDir(), "nylzo.db"), true))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := migrate(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatalf("migrate with a cancelled context: %v", err)
	}
}
