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
	dir := t.TempDir()
	b, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Put(t.Context(), "ref", bytes.NewReader(make([]byte, 4127)), 4127); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, disk.Dir, hex.EncodeToString([]byte("ref")))
	before := accessTime(t, p)
	time.Sleep(50 * time.Millisecond)
	rc, _, err := b.Open(t.Context(), "ref")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, rc)
	rc.Close()
	if after := accessTime(t, p); !after.Equal(before) {
		t.Fatalf("serving the blob recorded the read time in its atime: %v -> %v", before, after)
	}
}

func accessTime(t *testing.T, p string) time.Time {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(p, &st); err != nil {
		t.Fatal(err)
	}
	return time.Unix(st.Atim.Unix())
}
