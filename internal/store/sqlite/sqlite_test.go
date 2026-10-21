package sqlite_test

import (
	"path/filepath"
	"testing"

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
