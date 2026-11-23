package diskspace_test

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/server/diskspace"
)

func TestUploadsInProgressKeepTheirReservation(t *testing.T) {
	t.Parallel()
	const minFree = 100
	reserver := diskspace.NewReserver(
		t.TempDir(),
		minFree,
		func(string) (uint64, error) { return minFree + 10, nil },
		slog.New(slog.DiscardHandler),
	)
	release, ok := reserver.Reserve(6)
	if !ok {
		t.Fatal("the first upload did not fit")
	}
	if _, second := reserver.Reserve(6); second {
		t.Fatal("a second upload took the room the first one reserved")
	}
	release()
	again, ok := reserver.Reserve(10)
	if !ok {
		t.Fatal("a finished upload kept its reservation")
	}
	again()
	if _, last := reserver.Reserve(10); !last {
		t.Fatal("bytes stayed reserved after every upload ended")
	}
}

func TestNoMinimumAcceptsEveryUpload(t *testing.T) {
	t.Parallel()
	reserver := diskspace.NewReserver(
		t.TempDir(),
		0,
		func(string) (uint64, error) { return 0, errors.ErrUnsupported },
		slog.New(slog.DiscardHandler),
	)
	release, ok := reserver.Reserve(1 << 40)
	if !ok {
		t.Fatal("an upload was refused without a minimum of free space")
	}
	release()
}

func TestUnmeasuredOrNegativeRoomRefusesUploadsAndLogsOnce(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		minFree int64
		size    int64
		free    diskspace.FreeFunc
		event   string
	}{
		"unmeasured":       {1, 1, func(string) (uint64, error) { return 0, errors.ErrUnsupported }, "disk-free"},
		"negative minimum": {-1, 1, func(string) (uint64, error) { return 1 << 40, nil }, "disk-full"},
		"negative size":    {1, -10, func(string) (uint64, error) { return 1 << 40, nil }, "disk-full"},
	} {
		var logged bytes.Buffer
		reserver := diskspace.NewReserver(
			t.TempDir(),
			test.minFree,
			test.free,
			slog.New(slog.NewTextHandler(&logged, nil)),
		)
		for range 2 {
			if _, ok := reserver.Reserve(test.size); ok {
				t.Errorf("%s: an upload fit", name)
			}
		}
		if count := strings.Count(logged.String(), "msg="+test.event); count != 1 {
			t.Errorf("%s: logged %q %d times: %s", name, test.event, count, logged.String())
		}
	}
}

func TestFreeMeasuresOnlyADirectoryThatExists(t *testing.T) {
	t.Parallel()
	if _, err := diskspace.Free(t.TempDir()); diskspace.Measurable && err != nil {
		t.Fatalf("the free space of a directory: %v", err)
	}
	if _, err := diskspace.Free(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing directory has free space")
	}
}
