package erase

import (
	"errors"
	"io/fs"
	"testing"
)

var errDisk = errors.New("disk failure")

type failingDisk struct{}

func (failingDisk) Stat() (fs.FileInfo, error) { return nil, errDisk }

func (failingDisk) WriteAt([]byte, int64) (int, error) { return 0, errDisk }

func TestADiskFailureStopsTheOverwrite(t *testing.T) {
	t.Parallel()
	if _, err := regularSize(failingDisk{}); !errors.Is(err, errDisk) {
		t.Fatalf("stat: %v", err)
	}
	if err := writeZeros(failingDisk{}, 0, BlockLen); !errors.Is(err, errDisk) {
		t.Fatalf("write: %v", err)
	}
}
