package disk_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store/disk"
)

func newBlobs(t *testing.T, directory string) *disk.Blobs {
	t.Helper()
	blobs, err := disk.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	return blobs
}

func writeAged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.Now().Add(-age)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func TestBlobFilesAreLowerHexWithMinuteTimes(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	ctx := t.Context()
	for _, ref := range []string{"AbCdEfGhIjKlMnOpQrStUw", "abcdefghijklmnopqrstuw", "../escape"} {
		if err := blobs.Put(ctx, ref, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := blobs.Put(ctx, "../escape", strings.NewReader("y"), 1); err == nil {
		t.Fatal("a conflicting Put was accepted")
	}
	if err := blobs.Put(ctx, "short", strings.NewReader("sh"), 5); err == nil {
		t.Fatal("a short Put was accepted")
	}
	before := time.Now()
	if temporary, err := os.ReadDir(
		filepath.Join(directory, disk.Directory, "tmp"),
	); err != nil ||
		len(temporary) != 0 {
		t.Fatalf("temporary files left: %v %v", temporary, err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, disk.Directory))
	if err != nil {
		t.Fatal(err)
	}
	lowerHex := regexp.MustCompile(`^[0-9a-f]+$`)
	files := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files++
		info, infoErr := entry.Info()
		if infoErr != nil || !lowerHex.MatchString(entry.Name()) {
			t.Fatalf("blob file %q is not named in lowercase hex: %v", entry.Name(), infoErr)
		}
		if modified := info.ModTime(); !modified.Equal(modified.Truncate(time.Minute)) || modified.After(before) ||
			before.Sub(modified) > time.Minute+time.Second {
			t.Fatalf("a blob file carries the time of its upload: %v", modified)
		}
	}
	if files != 3 {
		t.Fatalf("want 3 blob files, got %d", files)
	}
}

func TestPurgeTempRemovesOnlyStaleFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	temporary := filepath.Join(directory, disk.Directory, "tmp")
	stale, fresh := filepath.Join(temporary, "blob-stale"), filepath.Join(temporary, "blob-fresh")
	writeAged(t, stale, time.Hour)
	writeAged(t, fresh, 0)
	purged, err := blobs.PurgeTemp(t.Context(), time.Now().Add(-30*time.Minute))
	if err != nil || purged != 1 {
		t.Fatalf("purge: %d %v", purged, err)
	}
	if _, err = os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a stale temporary file survived")
	}
	if _, err = os.Stat(fresh); err != nil {
		t.Fatal("a fresh temporary file was removed")
	}
}

func TestPurgeTempReportsFailedRemovals(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	temporary := filepath.Join(directory, disk.Directory, "tmp")
	writeAged(t, filepath.Join(temporary, "blob-stale"), time.Hour)
	if err := os.Chmod(temporary, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(temporary, 0o700) })
	if purged, err := blobs.PurgeTemp(t.Context(), time.Now().Add(-30*time.Minute)); err == nil || purged != 0 {
		t.Fatalf("a temporary file that cannot be removed must fail the sweep step: %d %v", purged, err)
	}
}

func TestDeleteBatchFailsEveryRefWhenTheSyncFails(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	if err := blobs.Put(t.Context(), "a", strings.NewReader("x"), 1); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(directory, disk.Directory)); err != nil {
		t.Fatal(err)
	}
	if errs := blobs.DeleteBatch(t.Context(), []string{"a", "b"}); len(errs) != 2 || errs[0] == nil || errs[1] == nil {
		t.Fatalf("a delete that could not be synced must not count as done: %v", errs)
	}
}
