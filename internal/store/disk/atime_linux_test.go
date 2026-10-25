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
	path := filepath.Join(directory, disk.Dir, hex.EncodeToString([]byte("ref")))
	before := accessTime(t, path)
	time.Sleep(50 * time.Millisecond)
	content, _, err := blobs.Open(t.Context(), "ref")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, content)
	content.Close()
	if after := accessTime(t, path); !after.Equal(before) {
		t.Fatalf("serving the blob recorded the read time in its atime: %v -> %v", before, after)
	}
}

func accessTime(t *testing.T, path string) time.Time {
	t.Helper()
	var status syscall.Stat_t
	if err := syscall.Stat(path, &status); err != nil {
		t.Fatal(err)
	}
	return time.Unix(status.Atim.Unix())
}
