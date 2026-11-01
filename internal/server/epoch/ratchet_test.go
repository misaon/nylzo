package epoch_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

// The seed file must hold nothing that makes the share of an hour that has passed, and it fills one block (L2).
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
	oldSeed := before[epoch.SeedOffset : epoch.SeedOffset+epoch.SeedLen]
	if len(after) != 4096 || bytes.Contains(after, oldSeed) || !os.SameFile(info, afterInfo) {
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

// L3: a missing directory may be a volume that failed to mount, so only an empty one gets a new ratchet.
func TestStartUpRefusesAMissingDirectoryAndADamagedSeedAndReplacesALostOne(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "unmounted")
	for _, create := range []bool{true, false} {
		if _, _, err := epoch.Open(
			missing,
			start,
			epoch.Options{Create: create},
		); !errors.Is(
			err,
			epoch.ErrNoDirectory,
		) {
			t.Fatalf("a missing directory with Create %v: %v", create, err)
		}
	}
	if _, err := epoch.Inspect(missing); !errors.Is(err, epoch.ErrNoDirectory) {
		t.Fatalf("inspecting a missing directory: %v", err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, epoch.FileName)
	if ratchet, created, err := epoch.Open(directory, start, epoch.Options{}); ratchet != nil || created || err != nil {
		t.Fatalf("without Create a missing seed must start nothing: %v", err)
	}
	if state, err := epoch.Inspect(directory); state != epoch.StateNoSeed || err != nil {
		t.Fatalf("an empty directory: %v %v", state, err)
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
	if state, inspectErr := epoch.Inspect(directory); state != epoch.StateSeed || inspectErr != nil {
		t.Fatalf("a directory with a seed: %v %v", state, inspectErr)
	}
	if err = os.WriteFile(path, []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = epoch.Open(directory, start, epoch.Options{Create: true}); !errors.Is(err, epoch.ErrCorrupt) {
		t.Fatalf("a damaged seed: %v", err)
	}
	if _, err = epoch.Inspect(directory); !errors.Is(err, epoch.ErrCorrupt) {
		t.Fatalf("inspecting a damaged seed: %v", err)
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

// L1: the seed opens every message that expires later, so it gets the protection of the pepper.
func TestSeedAndDirectoryMustBePrivate(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX modes")
	}
	for _, testCase := range []struct {
		name    string
		prepare func(t *testing.T, directory string) string
		want    error
	}{
		{"a seed others can read", func(t *testing.T, directory string) string {
			t.Helper()
			chmod(t, filepath.Join(directory, epoch.FileName), 0o644)
			return directory
		}, epoch.ErrExposed},
		{"a seed that is a link", func(t *testing.T, directory string) string {
			t.Helper()
			path := filepath.Join(directory, epoch.FileName)
			moved := filepath.Join(t.TempDir(), "seed")
			if err := os.Rename(path, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, path); err != nil {
				t.Fatal(err)
			}
			return directory
		}, epoch.ErrSymlink},
		{"a directory that is a link", func(t *testing.T, directory string) string {
			t.Helper()
			link := filepath.Join(t.TempDir(), "epoch")
			if err := os.Symlink(directory, link); err != nil {
				t.Fatal(err)
			}
			return link
		}, epoch.ErrSymlink},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			open(t, directory, start)
			target := testCase.prepare(t, directory)
			if _, _, err := epoch.Open(target, start, epoch.Options{Create: true}); !errors.Is(err, testCase.want) {
				t.Fatalf("open: %v, want %v", err, testCase.want)
			}
			if _, err := epoch.Inspect(target); !errors.Is(err, testCase.want) {
				t.Fatalf("inspect: %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestOpenNarrowsADirectoryOthersCanList(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX modes")
	}
	directory := t.TempDir()
	chmod(t, directory, 0o755)
	if _, err := epoch.Inspect(directory); err != nil {
		t.Fatalf("inspect changes nothing and refuses nothing serve would narrow: %v", err)
	}
	open(t, directory, start)
	if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the epoch directory keeps mode %v: %v", info.Mode().Perm(), err)
	}
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
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
