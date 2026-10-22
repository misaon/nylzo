package sqlite_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

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

func TestOpenMigratesOlderSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, version INTEGER NOT NULL, kdf_id INTEGER NOT NULL, flags INTEGER NOT NULL, state TEXT NOT NULL,
		created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ttl_s INTEGER NOT NULL, opens_max INTEGER NOT NULL, opens_remaining INTEGER NOT NULL,
		pepper_id INTEGER NOT NULL, read_mac BLOB NOT NULL, manage_mac BLOB NOT NULL, verifier_mac BLOB, size INTEGER NOT NULL, blob_ref TEXT,
		day_fail_count INTEGER NOT NULL DEFAULT 0, day_fail_date TEXT NOT NULL DEFAULT '', fail_count_total INTEGER NOT NULL DEFAULT 0, first_opened_at INTEGER);
		CREATE INDEX messages_release ON messages(opens_remaining, blob_ref);
		INSERT INTO messages (id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, size, blob_ref)
		VALUES ('AAAAAAAAAAAAAAAAAAAAAA', 1, 0, 0, 'available', 0, 9999999999999, 60, 1, 1, 7, x'00', x'00', 64, 'AAAAAAAAAAAAAAAAAAAAAA');`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open an older database: %v", err)
	}
	defer s.Close()
	ctx := t.Context()
	state, ref, err := s.MarkEnding(ctx, "AAAAAAAAAAAAAAAAAAAAAA", store.StateCancelled, time.Unix(0, 0))
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
	var stale int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'messages_release'`).Scan(&stale); err != nil || stale != 0 {
		t.Fatalf("the old release index must be dropped: %d %v", stale, err)
	}
}
