package epoch_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/epoch"
)

// lockedKiB reads how much of the mapping of path /proc/self/smaps shows as locked, and whether it is mapped at all.
func lockedKiB(t *testing.T, path string) (string, bool) {
	t.Helper()
	smaps, err := os.Open("/proc/self/smaps")
	if err != nil {
		t.Fatal(err)
	}
	defer smaps.Close()
	lines := bufio.NewScanner(smaps)
	inside, mapped := false, false
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		switch {
		case len(fields) >= 6 && strings.Contains(fields[0], "-"):
			inside = fields[len(fields)-1] == path
			mapped = mapped || inside
		case inside && len(fields) >= 2 && fields[0] == "Locked:":
			return fields[1], true
		default:
		}
	}
	return "", mapped
}

func TestPinLocksTheSeedPagesUntilClose(t *testing.T) {
	directory := t.TempDir()
	ratchet := open(t, directory, time.Now())
	path := filepath.Join(directory, epoch.FileName)
	if err := ratchet.Pin(); err != nil {
		t.Fatal(err)
	}
	page := os.Getpagesize()
	want := strconv.Itoa((epoch.FileLen + page - 1) / page * page / 1024)
	if locked, mapped := lockedKiB(t, path); !mapped || locked != want {
		t.Fatalf("the whole seed file must be locked: mapped %v, locked %q kB, want %s kB", mapped, locked, want)
	}
	ratchet.Close()
	if _, mapped := lockedKiB(t, path); mapped {
		t.Fatal("Close must release the mapping")
	}
}
