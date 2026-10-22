package sqlite_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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
				err = s.Close()
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
