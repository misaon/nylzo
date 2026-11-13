package disk_test

import (
	"bytes"
	"encoding/hex"
	"io"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/misaon/nylzo/internal/store/disk"
)

func TestBlobReadKeepsAccessTime(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	if _, err := blobs.PutExpiring(
		t.Context(),
		"ref",
		bytes.NewReader(make([]byte, 4127)),
		4127,
		time.Now().Add(time.Hour),
	); err != nil {
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
	for _, reference := range []string{"first", "second"} {
		if _, err := blobs.PutExpiring(
			t.Context(),
			reference,
			bytes.NewReader([]byte(reference)),
			int64(len(reference)),
			time.Now().Add(time.Hour),
		); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Join(directory, disk.Directory, hex.EncodeToString([]byte(reference))))
		time.Sleep(50 * time.Millisecond)
	}
	restamped := time.Now()
	if err := blobs.Restamp(t.Context(), restamped); err != nil {
		t.Fatal(err)
	}
	coarseClock := 20 * time.Millisecond
	for _, path := range append(paths, filepath.Join(directory, disk.Directory)) {
		status := fileTimes(t, path)
		if changed := time.Unix(status.Ctim.Unix()); changed.Before(restamped.Add(-coarseClock)) {
			t.Fatalf("%s keeps the ctime of its upload: %v", filepath.Base(path), changed)
		}
	}
}

func TestABlobIsBornBeforeItsUpload(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	time.Sleep(100 * time.Millisecond)
	started := time.Now()
	if _, err := blobs.PutExpiring(
		t.Context(),
		"ref",
		strings.NewReader("x"),
		1,
		time.Now().Add(time.Hour),
	); err != nil {
		t.Fatal(err)
	}
	var status unix.Statx_t
	path := filepath.Join(directory, disk.Directory, hex.EncodeToString([]byte("ref")))
	if err := unix.Statx(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BTIME, &status); err != nil {
		t.Fatal(err)
	}
	if status.Mask&unix.STATX_BTIME == 0 {
		t.Skip("the file system records no birth time")
	}
	coarseClock := 20 * time.Millisecond
	if born := time.Unix(status.Btime.Sec, int64(status.Btime.Nsec)); !born.Before(started.Add(-coarseClock)) {
		t.Fatalf("the blob's birth time %v dates its upload at %v", born, started)
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
