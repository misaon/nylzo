package epoch

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

func replaceSeed(t *testing.T, directory string, replace func(path string)) {
	t.Helper()
	path := filepath.Join(directory, FileName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	replace(path)
}

func writeSeed(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestASeedThatWentOrChangedFailsTheIssueAndTheShare(t *testing.T) {
	t.Parallel()
	locator := seal.LocatorOf([]byte("message"))
	shareEpoch := Minute(internalStart) + 1
	for name, testCase := range map[string]struct {
		replace func(t *testing.T, path string)
		want    error
		inspect error
	}{
		"a removed seed": {func(*testing.T, string) {}, fs.ErrNotExist, nil},
		"a directory": {func(t *testing.T, path string) {
			t.Helper()
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, nil, ErrNotRegular},
		"a seed longer than a seed file": {func(t *testing.T, path string) {
			t.Helper()
			writeSeed(t, path, make([]byte, FileLen+1))
		}, ErrCorrupt, ErrCorrupt},
		"a damaged seed": {func(t *testing.T, path string) {
			t.Helper()
			writeSeed(t, path, make([]byte, FileLen))
		}, ErrCorrupt, ErrCorrupt},
	} {
		directory := t.TempDir()
		ratchet := openInternal(t, directory, internalStart)
		replaceSeed(t, directory, func(path string) { testCase.replace(t, path) })
		_, _, err := ratchet.Issue(shareEpoch, locator, internalStart)
		if err == nil || (testCase.want != nil && !errors.Is(err, testCase.want)) {
			t.Fatalf("%s: issue %v", name, err)
		}
		if _, err = Inspect(directory); !errors.Is(err, testCase.inspect) {
			t.Fatalf("%s: inspect %v", name, err)
		}
		_, err = ratchet.Share(GenerationID{1}, shareEpoch, locator, internalStart)
		if err == nil || errors.Is(err, ErrShareGone) {
			t.Fatalf("%s: share %v", name, err)
		}
	}
}

func TestASeedOthersCanReadFailsTheShare(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	ratchet := openInternal(t, directory, internalStart)
	if err := os.Chmod(filepath.Join(directory, FileName), 0o644); err != nil {
		t.Fatal(err)
	}
	locator := seal.LocatorOf([]byte("message"))
	if _, err := ratchet.Share(GenerationID{1}, Minute(internalStart)+1, locator, internalStart); !errors.Is(
		err,
		ErrExposed,
	) {
		t.Fatalf("a seed others can read: %v", err)
	}
}

func TestAFileWithoutTheGenerationOfTheHourIsLoadedAgain(t *testing.T) {
	t.Parallel()
	earlier := openInternal(t, t.TempDir(), internalStart)
	later := internalStart.Add(2 * time.Hour)
	directory := t.TempDir()
	ratchet := openInternal(t, directory, later)
	locator := seal.LocatorOf([]byte("message"))
	shareEpoch := Minute(later) + 1
	before, _, err := ratchet.Issue(shareEpoch, locator, later)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(earlier.path)
	if err != nil {
		t.Fatal(err)
	}
	writeSeed(t, filepath.Join(directory, FileName), content)
	issued, _, err := ratchet.Issue(shareEpoch, locator, later)
	if err != nil || issued == before || !ratchet.Covers(earlier.Generations()[0], shareEpoch) {
		t.Fatalf("the ratchet kept a generation the file no longer holds: %v", err)
	}
}

func TestDecodeDropsAGenerationThatEndedBeforeTheNewestSlot(t *testing.T) {
	t.Parallel()
	var ended, current state
	ended.advance(1000*minutesPerHour + 17)
	current.advance((1000+Span+1)*minutesPerHour + 3)
	content := encode(&ended)
	newest := encode(&current)
	copy(content[2*slotLen:3*slotLen], newest[slotLen:2*slotLen])
	var decoded state
	if !decode(content, &decoded) || decoded.named(ended.slots[0].id) != nil ||
		decoded.named(current.slots[0].id) == nil {
		t.Fatal("a generation past its last epoch survived the decode")
	}
}

func TestReadContentReportsAReadThatFails(t *testing.T) {
	t.Parallel()
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if content, readErr := readContent(file); content != nil || readErr == nil || errors.Is(readErr, ErrCorrupt) {
		t.Fatalf("a read that failed: %v", readErr)
	}
}

func TestAClosedOrPinnedRatchetPinsNothingAgain(t *testing.T) {
	t.Parallel()
	ratchet := openInternal(t, t.TempDir(), internalStart)
	if err := ratchet.Pin(); err != nil {
		t.Fatal(err)
	}
	pinned := ratchet.pinned
	if err := ratchet.Pin(); err != nil || (len(pinned) > 0 && &pinned[0] != &ratchet.pinned[0]) {
		t.Fatalf("a second pin mapped the seed again: %v", err)
	}
	ratchet.Close()
	if err := ratchet.Pin(); !errors.Is(err, errClosed) {
		t.Fatalf("a closed ratchet pinned its seed: %v", err)
	}
}

// The checks that need another user run only without root, which opens and owns everything.
func TestASeedThisUserCannotReadOrWriteIsRefused(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads and writes whatever the mode")
	}
	if _, err := Inspect("/"); !errors.Is(err, ErrForeignOwner) {
		t.Fatalf("a directory of another user: %v", err)
	}
	if err := checkSeed("/etc/passwd"); !errors.Is(err, ErrForeignOwner) {
		t.Fatalf("a seed of another user: %v", err)
	}
	directory := t.TempDir()
	openInternal(t, directory, internalStart)
	path := filepath.Join(directory, FileName)
	if err := os.Chmod(path, 0o200); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(directory); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("inspecting a seed this user cannot read: %v", err)
	}
	if _, _, err := Open(directory, internalStart, Options{Create: true}); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("opening a seed this user cannot read: %v", err)
	}
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o700) })
	if _, _, err := Open(readOnly, internalStart, Options{Create: true}); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("creating a seed where this user cannot write: %v", err)
	}
}

func TestADirectoryPathThroughAFileIsRefused(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "file")
	writeSeed(t, file, nil)
	if _, err := Inspect(filepath.Join(file, "epoch")); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("a directory below a file: %v", err)
	}
}

var errFault = errors.New("fault")

type failingStat struct{}

func (failingStat) Stat() (fs.FileInfo, error) { return nil, errFault }

func TestAFaultOfTheOpenSeedIsReported(t *testing.T) {
	t.Parallel()
	if err := eraseWhileNamed(t.TempDir(), failingStat{}); !errors.Is(err, errFault) {
		t.Fatalf("stat of the held file: %v", err)
	}
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if content, readErr := readLocked(file, func(*os.File) error { return errFault }); content != nil ||
		!errors.Is(readErr, errFault) {
		t.Fatalf("lock: %v", readErr)
	}
}

func TestEraseWhileNamedErasesOnlyTheHeldFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, FileName)
	writeSeed(t, path, []byte("held"))
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = eraseWhileNamed(path, file); err != nil {
		t.Fatalf("a removed name: %v", err)
	}
	writeSeed(t, path, []byte("newer"))
	if err = eraseWhileNamed(path, file); err != nil {
		t.Fatalf("a name that leads to another file: %v", err)
	}
	if content, readErr := os.ReadFile(path); string(content) != "newer" || readErr != nil {
		t.Fatalf("the erase reached a seed written since: %q %v", content, readErr)
	}
	if err = eraseWhileNamed(filepath.Join(path, "below"), file); err == nil {
		t.Fatal("a name below a file")
	}
	held, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err = eraseWhileNamed(path, held); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the held file kept its name: %v", err)
	}
}
