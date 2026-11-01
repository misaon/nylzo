package epoch

import (
	"errors"
	"os"
	"testing"
	"time"
)

// L6: after a failed overwrite the next advance writes again, even within the same hour, and memory has
// already left the passed hour.
func TestAFailedOverwriteIsWrittenAgain(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 1, 10, 20, 0, 0, time.UTC)
	ratchet, _, err := Open(t.TempDir(), start, Options{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ratchet.Close)
	diskFull := errors.New("no space left on device")
	written := 0
	ratchet.replace = func(path string, content []byte) error {
		written++
		if written == 1 {
			return diskFull
		}
		return os.WriteFile(path, content, 0o600)
	}
	next := start.Add(time.Hour)
	if err = ratchet.Advance(next); !errors.Is(err, diskFull) {
		t.Fatalf("the failed overwrite: %v", err)
	}
	if ratchet.hour != Hour(next) || !ratchet.unsynced {
		t.Fatalf("memory stayed at hour %d after a failed overwrite", ratchet.hour)
	}
	if err = ratchet.Advance(next); err != nil || written != 2 || ratchet.unsynced {
		t.Fatalf("the retry within the hour wrote %d times: %v", written, err)
	}
	content, err := os.ReadFile(ratchet.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, hour, seed, ok := decode(content); !ok || hour != Hour(next) || seed != ratchet.seed {
		t.Fatalf("the file holds hour %d", hour)
	}
	if err = ratchet.Advance(next.Add(time.Minute)); err != nil || written != 2 {
		t.Fatalf("a synced ratchet wrote again within its hour: %d %v", written, err)
	}
}
