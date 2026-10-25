package diskspace_test

import (
	"log/slog"
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
