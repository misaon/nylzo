package epoch_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/misaon/nylzo/internal/epoch"
)

// withLimit changes a limit of the whole process, so its tests must not run in parallel.
func withLimit(t *testing.T, resource int, value uint64, run func()) {
	t.Helper()
	var limit unix.Rlimit
	if err := unix.Getrlimit(resource, &limit); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setrlimit(resource, &unix.Rlimit{Cur: value, Max: limit.Max}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Setrlimit(resource, &limit); err != nil {
			t.Fatal(err)
		}
	}()
	run()
}

func TestASeedThatCannotBeWrittenIsRemoved(t *testing.T) {
	directory := t.TempDir()
	var err error
	withLimit(t, unix.RLIMIT_FSIZE, epoch.FileLen-1, func() {
		_, _, err = epoch.Open(directory, time.Now(), epoch.Options{Create: true})
	})
	if !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("a seed longer than the file size limit: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(directory, epoch.FileName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a seed that was cut short stayed: %v", statErr)
	}
}

func TestPinFailsWithoutLockedMemory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root locks memory beyond the limit")
	}
	directory := t.TempDir()
	ratchet := open(t, directory, time.Now())
	var err error
	withLimit(t, unix.RLIMIT_MEMLOCK, 0, func() { err = ratchet.Pin() })
	if err == nil {
		t.Fatal("the seed was pinned without locked memory")
	}
	if _, mapped := lockedKiB(t, filepath.Join(directory, epoch.FileName)); mapped {
		t.Fatal("a failed pin left the seed mapped")
	}
}
