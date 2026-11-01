package disk_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/store"
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

func TestBlobFilesAreLowerHexWithQuarterHourTimes(t *testing.T) {
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
		if modified := info.ModTime(); !modified.Equal(modified.Truncate(store.BlobPurgeStep)) ||
			modified.Sub(before) > store.BlobPurgeStep || before.Sub(modified) > time.Second {
			t.Fatalf("a blob file must carry the end of the quarter hour of its upload: %v", modified)
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

// The test keeps each file open, so it reads the inode's own blocks after the name is gone.
func TestEveryDeletionOverwritesTheEraseBlockWhereItLies(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	root := filepath.Join(directory, disk.Directory)
	content := bytes.Repeat([]byte{0xA5}, 2*erase.BlockLen)
	opened := map[string]*os.File{}
	for _, ref := range []string{"deleted", "batched", "stale-upload"} {
		if err := blobs.Put(t.Context(), ref, bytes.NewReader(content), int64(len(content))); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(filepath.Join(root, hex.EncodeToString([]byte(ref))))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { file.Close() })
		opened[ref] = file
	}
	stale := filepath.Join(root, "tmp", "blob-stale")
	if err := os.Rename(filepath.Join(root, hex.EncodeToString([]byte("stale-upload"))), stale); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Delete(t.Context(), "deleted"); err != nil {
		t.Fatal(err)
	}
	if errs := blobs.DeleteBatch(t.Context(), []string{"batched"}); errs[0] != nil {
		t.Fatal(errs[0])
	}
	if _, err := blobs.PurgeTemp(t.Context(), time.Now().Add(-store.BlobGrace)); err != nil {
		t.Fatal(err)
	}
	for ref, file := range opened {
		left := make([]byte, len(content))
		if _, err := file.ReadAt(
			left,
			0,
		); err != nil ||
			!bytes.Equal(left[:erase.BlockLen], make([]byte, erase.BlockLen)) {
			t.Errorf("%s: the erase block survived the deletion in the file's own blocks: %v", ref, err)
		}
	}
}

// A second link to each file shows its bytes after EraseAll: only an overwrite in place turns them into zeros.
func TestEraseAllOverwritesEveryReleasedBlobInFullBeforeItGoes(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	newBlobs(t, directory)
	keep := t.TempDir()
	content := bytes.Repeat([]byte{0xa5}, 3*erase.BlockLen+17)
	names := []string{"7265662d6f6e65", filepath.Join("tmp", "blob-1")}
	for _, name := range names {
		path := filepath.Join(directory, disk.Directory, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, filepath.Join(keep, filepath.Base(name))); err != nil {
			t.Fatal(err)
		}
	}
	if err := disk.EraseAll(directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		left, err := os.ReadFile(filepath.Join(keep, filepath.Base(name)))
		if err != nil || !bytes.Equal(left, make([]byte, len(content))) {
			t.Fatalf("%s kept some of its bytes: %v", name, err)
		}
		if _, err = os.Stat(filepath.Join(directory, disk.Directory, name)); !os.IsNotExist(err) {
			t.Fatalf("%s is still in the blob directory: %v", name, err)
		}
	}
}
