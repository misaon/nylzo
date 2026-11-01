package epoch_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/vectors"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/epoch"
)

var start = time.Date(2026, 11, 1, 10, 20, 0, 0, time.UTC)

func open(t *testing.T, directory string, now time.Time) *epoch.Ratchet {
	t.Helper()
	ratchet, _, err := epoch.Open(directory, now, epoch.Options{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ratchet.Close)
	return ratchet
}

func TestEpochVectors(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../spec/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file vectors.File
	if err = json.Unmarshal(data, &file); err != nil || len(file.Server.Epochs) < 2 {
		t.Fatalf("epoch vectors: %v", err)
	}
	seed := epoch.Seed(decode(t, file.Server.Epochs[0].Seed))
	for _, vector := range file.Server.Epochs {
		key := epoch.Key(&seed)
		share := epoch.ShareOf(&key, seal.Locator(decode(t, vector.Locator)))
		if hex.EncodeToString(seed[:]) != vector.Seed || hex.EncodeToString(key[:]) != vector.EpochKey ||
			hex.EncodeToString(share[:]) != vector.Share {
			t.Fatalf("hour %d differs", vector.Hour)
		}
		seed = epoch.Next(&seed)
	}
}

func decode(t *testing.T, text string) [32]byte {
	t.Helper()
	decoded, err := hex.DecodeString(text)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("%q: %v", text, err)
	}
	return [32]byte(decoded)
}

func TestF3ShareOfAPassedHourIsGone(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	ratchet := open(t, directory, start)
	locator := seal.LocatorOf([]byte("message"))
	hour := epoch.Hour(start)
	live, err := ratchet.Share(ratchet.ID(), hour+2, locator, start)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ratchet.Share(ratchet.ID(), hour+2, locator, start.Add(90*time.Minute))
	if err != nil || again != live {
		t.Fatalf("a share must stay the same while its hour lasts: %v", err)
	}
	if _, err = ratchet.Share(ratchet.ID(), hour+2, locator, epoch.End(hour+2)); !errors.Is(err, epoch.ErrShareGone) {
		t.Fatalf("the share outlived its hour: %v", err)
	}
	if _, err = ratchet.Share(epoch.RatchetID{1}, hour+5, locator, start); !errors.Is(err, epoch.ErrShareGone) {
		t.Fatalf("a share of another ratchet: %v", err)
	}
}

// The seed file must hold nothing that makes the share of an hour that has passed.
func TestF3AdvanceOverwritesTheSeedInPlace(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	ratchet := open(t, directory, start)
	path := filepath.Join(directory, epoch.FileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = ratchet.Advance(start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	oldSeed := before[len(before)-epoch.SeedLen:]
	if len(after) != len(before) || bytes.Contains(after, oldSeed) || !os.SameFile(info, afterInfo) {
		t.Fatal("the old seed is still in the file, or the file was replaced instead of overwritten")
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the seed file has mode %v", info.Mode().Perm())
	}
}

func TestRatchetsSharingAFileAgree(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	first := open(t, directory, start)
	second := open(t, directory, start)
	locator := seal.LocatorOf([]byte("message"))
	hour := epoch.Hour(start)
	if err := first.Advance(start.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	fromFirst, err := first.Share(first.ID(), hour+3, locator, start.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	fromSecond, err := second.Share(second.ID(), hour+3, locator, start.Add(2*time.Hour))
	if err != nil || fromSecond != fromFirst || first.ID() != second.ID() {
		t.Fatalf("two processes on one seed file disagree: %v", err)
	}
}

func TestSeedFileLossAndDamage(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, epoch.FileName)
	if ratchet, created, err := epoch.Open(directory, start, epoch.Options{}); ratchet != nil || created || err != nil {
		t.Fatalf("without Create a missing seed must start nothing: %v", err)
	}
	first := open(t, directory, start)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ratchet, created, err := epoch.Open(directory, start, epoch.Options{Create: true})
	if err != nil || !created || ratchet.ID() == first.ID() {
		t.Fatalf("a lost seed must start a new ratchet: %v", err)
	}
	ratchet.Close()
	if err = os.WriteFile(path, []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = epoch.Open(directory, start, epoch.Options{Create: true}); !errors.Is(err, epoch.ErrCorrupt) {
		t.Fatalf("a damaged seed: %v", err)
	}
	refuse := errors.New("refused")
	_, _, err = epoch.Open(t.TempDir(), start, epoch.Options{
		Create:          true,
		CheckFileSystem: func(string) error { return refuse },
	})
	if !errors.Is(err, refuse) {
		t.Fatalf("the file system check did not run: %v", err)
	}
}

func TestShareBindsTheLocator(t *testing.T) {
	t.Parallel()
	ratchet := open(t, t.TempDir(), start)
	hour := epoch.Hour(start)
	first, err := ratchet.Share(ratchet.ID(), hour, newLocator(), start)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ratchet.Share(ratchet.ID(), hour, newLocator(), start)
	if err != nil || first == second {
		t.Fatalf("two messages got one share: %v", err)
	}
}

func newLocator() seal.Locator {
	id := core.NewID()
	return seal.LocatorOf(id[:])
}
