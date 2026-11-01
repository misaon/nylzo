package disk_test

import (
	"bytes"
	"encoding/hex"
	"io"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store/disk"
)

func TestBlobReadKeepsAccessTime(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	if err := blobs.Put(t.Context(), "ref", bytes.NewReader(make([]byte, 4127)), 4127); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, disk.Directory, hex.EncodeToString([]byte("ref")))
	before := fileTimes(t, path).Atim
	time.Sleep(50 * time.Millisecond)
	content, _, err := blobs.Open(t.Context(), "ref")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, content)
	content.Close()
	if after := fileTimes(t, path).Atim; after != before {
		t.Fatalf("serving the blob recorded the read time in its atime: %v -> %v", before, after)
	}
}

func TestRestampHidesWhenEachBlobWasUploaded(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	var paths []string
	for _, ref := range []string{"first", "second"} {
		if err := blobs.Put(t.Context(), ref, bytes.NewReader([]byte(ref)), int64(len(ref))); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Join(directory, disk.Directory, hex.EncodeToString([]byte(ref))))
		time.Sleep(50 * time.Millisecond)
	}
	restamped := time.Now()
	if err := blobs.Restamp(t.Context(), restamped); err != nil {
		t.Fatal(err)
	}
	coarseClock := 20 * time.Millisecond
	for _, path := range append(paths, filepath.Join(directory, disk.Directory)) {
		if changed := time.Unix(fileTimes(t, path).Ctim.Unix()); changed.Before(restamped.Add(-coarseClock)) {
			t.Fatalf("%s keeps the ctime of its upload: %v", filepath.Base(path), changed)
		}
	}
}

func fileTimes(t *testing.T, path string) syscall.Stat_t {
	t.Helper()
	var status syscall.Stat_t
	if err := syscall.Stat(path, &status); err != nil {
		t.Fatal(err)
	}
	return status
}
