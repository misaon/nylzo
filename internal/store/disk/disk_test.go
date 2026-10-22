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

func TestTempDirStaysEmptyAfterPut(t *testing.T) {
	dir := t.TempDir()
	b, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := b.Put(ctx, "a", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(ctx, "a", strings.NewReader("other"), 5); err == nil {
		t.Fatal("conflicting Put accepted")
	}
	if err := b.Put(ctx, "b", strings.NewReader("sh"), 5); err == nil {
		t.Fatal("short Put accepted")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "blobs", "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temp entries left: %v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "blobs", "61")); err != nil {
		t.Fatal(err)
	}
}

func TestBlobNamesAreLowerHex(t *testing.T) {
	dir := t.TempDir()
	b, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"AbCdEfGhIjKlMnOpQrStUw", "abcdefghijklmnopqrstuw", "../escape"} {
		if err := b.Put(t.Context(), ref, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	hexName := regexp.MustCompile(`^[0-9a-f]+$`)
	files := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files++
		if !hexName.MatchString(e.Name()) {
			t.Fatalf("blob file name %q is not lowercase hex", e.Name())
		}
	}
	if files != 3 {
		t.Fatalf("want 3 blob files, got %d", files)
	}
}

func TestPurgeTempKeepsFreshFiles(t *testing.T) {
	dir := t.TempDir()
	b, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "blobs", "tmp")
	old, fresh := filepath.Join(tmp, "blob-old"), filepath.Join(tmp, "blob-fresh")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	n, err := b.PurgeTemp(t.Context(), time.Now().Add(-30*time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("stale temp file survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh temp file removed")
	}
}

func TestPurgeTempReportsFailedRemovals(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	b, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "blobs", "tmp")
	stale := filepath.Join(tmp, "blob-stale")
	if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmp, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(tmp, 0o700) })
	if n, err := b.PurgeTemp(t.Context(), time.Now().Add(-30*time.Minute)); err == nil || n != 0 {
		t.Fatalf("a temp file that cannot be removed must fail the sweep step: %d %v", n, err)
	}
}
