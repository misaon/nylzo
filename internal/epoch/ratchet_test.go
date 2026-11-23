package epoch_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/vectors"
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
	data, err := os.ReadFile("../../spec/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file vectors.File
	if err = json.Unmarshal(data, &file); err != nil || len(file.Server.MinuteEpochs) < 4 {
		t.Fatalf("epoch vectors: %v", err)
	}
	checkMinuteVectors(t, file.Server.MinuteEpochs)
}

// checkMinuteVectors walks the chain of minutes from the first case and starts the chain of each new hour from the
// seed of that hour.
func checkMinuteVectors(t *testing.T, cases []vectors.MinuteEpochCase) {
	t.Helper()
	first := cases[0]
	hourSeed := epoch.Seed(decode(t, first.HourSeed))
	minuteSeed := epoch.FirstMinute(&hourSeed)
	minute := first.Minute
	for _, vector := range cases {
		for ; minute < vector.Minute; minute++ {
			minuteSeed = epoch.NextMinute(&minuteSeed)
			if (minute+1)%60 == 0 {
				hourSeed = epoch.Next(&hourSeed)
				minuteSeed = epoch.FirstMinute(&hourSeed)
			}
		}
		key := epoch.MinuteKey(&minuteSeed)
		share := epoch.ShareOf(&key, seal.Locator(decode(t, vector.Locator)))
		if hex.EncodeToString(hourSeed[:]) != vector.HourSeed ||
			hex.EncodeToString(minuteSeed[:]) != vector.MinuteSeed ||
			hex.EncodeToString(key[:]) != vector.EpochKey ||
			hex.EncodeToString(share[:]) != vector.Share {
			t.Fatalf("minute %d differs", vector.Minute)
		}
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

// A share lives exactly until the deadline that ends its epoch, the instant at which its message expires.
func TestShareGoesAtItsDeadline(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	ratchet := open(t, directory, start)
	locator := seal.LocatorOf([]byte("message"))
	deadline := start.Add(2*time.Hour + 7*time.Minute)
	shareEpoch := epoch.Of(deadline)
	if epoch.Deadline(shareEpoch) != deadline {
		t.Fatalf("epoch %d ends at %v, want %v", shareEpoch, epoch.Deadline(shareEpoch), deadline)
	}
	generation, live, err := ratchet.Issue(shareEpoch, locator, start)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ratchet.Share(generation, shareEpoch, locator, deadline.Add(-time.Nanosecond))
	if err != nil || again != live {
		t.Fatalf("a share must stay the same until its deadline: %v", err)
	}
	if _, err = ratchet.Share(generation, shareEpoch, locator, deadline); !errors.Is(err, epoch.ErrShareExpired) {
		t.Fatalf("the share outlived its deadline: %v", err)
	}
	_, err = ratchet.Share(epoch.GenerationID{1}, shareEpoch+5, locator, start)
	if !errors.Is(err, epoch.ErrShareGone) || errors.Is(err, epoch.ErrShareExpired) {
		t.Fatalf("a share of another generation is lost, not expired: %v", err)
	}
}

// The seed file must hold nothing that makes the share of a minute that has passed, and it fills whole blocks.
func TestAdvanceOverwritesTheSeedInPlace(t *testing.T) {
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
	if err = ratchet.Advance(start.Add(time.Hour + time.Minute)); err != nil {
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
	oldSeeds := epoch.Seeds(before)
	if len(after) != epoch.FileLen || len(oldSeeds) == 0 || !os.SameFile(info, afterInfo) {
		t.Fatal("the file was replaced instead of overwritten, or held no seed")
	}
	for _, oldSeed := range oldSeeds {
		if bytes.Contains(after, oldSeed[:]) {
			t.Fatal("a seed of the passed minutes is still in the file")
		}
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
	behind := open(t, directory, start)
	locator := seal.LocatorOf([]byte("message"))
	later := start.Add(2 * time.Hour)
	shareEpoch := epoch.Minute(later) + 3*60
	generation, fromFirst, err := first.Issue(shareEpoch, locator, later)
	if err != nil {
		t.Fatal(err)
	}
	fromSecond, err := second.Share(generation, shareEpoch, locator, later)
	if err != nil || fromSecond != fromFirst {
		t.Fatalf("two processes on one seed file disagree: %v", err)
	}
	if issuer, _, issueErr := second.Issue(shareEpoch, locator, later); issueErr != nil || issuer != generation {
		t.Fatalf("two processes issued one hour from two generations: %v", issueErr)
	}
	if held, holdsErr := behind.Holds(generation, start); holdsErr != nil || !held {
		t.Fatalf("a process behind its peer refused a generation the file holds: %v", holdsErr)
	}
}

// Every hour issues from a new generation, which keeps making its shares up to its last epoch and then goes.
func TestEveryHourIssuesFromANewGeneration(t *testing.T) {
	t.Parallel()
	ratchet := open(t, t.TempDir(), start)
	locator := seal.LocatorOf([]byte("message"))
	hour := epoch.Minute(start) / 60
	last := (hour+epoch.Span)*60 - 1
	first, issued, err := ratchet.Issue(last, locator, start)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = ratchet.Issue(last+1, locator, start); err == nil {
		t.Fatal("a generation issued a share past its span")
	}
	next, _, err := ratchet.Issue(last, locator, start.Add(time.Hour))
	if err != nil || next == first {
		t.Fatalf("a new hour issued from the generation of the hour before: %v", err)
	}
	if again, _, againErr := ratchet.Issue(last, locator, start.Add(80*time.Minute)); againErr != nil || again != next {
		t.Fatalf("one hour issued from two generations: %v", againErr)
	}
	if kept, keptErr := ratchet.Share(first, last, locator, epoch.Deadline(last-1)); keptErr != nil || kept != issued {
		t.Fatalf("a generation lost its share before its last epoch: %v", keptErr)
	}
	if _, err = ratchet.Share(first, last, locator, epoch.Deadline(last)); !errors.Is(err, epoch.ErrShareGone) ||
		slices.Contains(ratchet.Generations(), first) {
		t.Fatalf("a generation outlived its last epoch: %v", err)
	}
}

// A clock stepped forward starts the generation of a later hour early, so a seed copied then would predict its shares.
// Once the clock is back, an advance replaces that generation and reports it. A lookup never does, since its request
// may have read the clock before a slow upload, and neither does a clock up to ClockStep behind, as between processes
// that share the seed.
func TestAGenerationStartedByAClockAheadNeverIssuesOnceTheClockIsBack(t *testing.T) {
	t.Parallel()
	locator := seal.LocatorOf([]byte("message"))
	ahead := start.Add(3 * time.Hour)
	shareEpoch := epoch.Minute(ahead) + 60
	replaced := 0
	ratchet, _, err := epoch.Open(t.TempDir(), start, epoch.Options{Create: true, Replaced: func() { replaced++ }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ratchet.Close)
	early, _, err := ratchet.Issue(shareEpoch, locator, ahead)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ratchet.Holds(epoch.GenerationID{1}, start); err != nil {
		t.Fatal(err)
	}
	if err = ratchet.Advance(ahead.Add(-epoch.ClockStep)); err != nil {
		t.Fatal(err)
	}
	if kept, _, keptErr := ratchet.Issue(shareEpoch, locator, ahead); keptErr != nil || kept != early || replaced != 0 {
		t.Fatalf("a lookup with a clock read long ago or a clock ClockStep behind replaced the generation: %v", keptErr)
	}
	if err = ratchet.Advance(start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	renewed, _, err := ratchet.Issue(shareEpoch, locator, ahead)
	if err != nil || renewed == early || replaced != 1 {
		t.Fatalf("the generation that a clock ahead started issued after the clock was back: %v", err)
	}
	if _, err = ratchet.Share(early, shareEpoch, locator, ahead); !errors.Is(err, epoch.ErrShareGone) {
		t.Fatalf("a share of the replaced generation survived: %v", err)
	}
}

// Processes that share the seed replace the generation one after another while the clock is behind; each reads the
// file again before it issues, so none issues from a generation that only its memory holds.
func TestProcessesThatShareTheSeedIssueFromOneGenerationAfterAReplacement(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	locator := seal.LocatorOf([]byte("message"))
	ahead := start.Add(3 * time.Hour)
	shareEpoch := epoch.Minute(ahead) + 60
	first := open(t, directory, ahead)
	second := open(t, directory, ahead)
	for _, process := range []*epoch.Ratchet{first, second} {
		if err := process.Advance(start); err != nil {
			t.Fatal(err)
		}
	}
	fromFirst, _, err := first.Issue(shareEpoch, locator, ahead)
	if err != nil {
		t.Fatal(err)
	}
	fromSecond, _, err := second.Issue(shareEpoch, locator, ahead)
	if err != nil || fromFirst != fromSecond {
		t.Fatalf("two processes issued one hour from two generations: %v", err)
	}
}

// A process that never saw the clock behind, such as serve while a sweep-once ran during a short backward step,
// follows the replacement that the other process wrote before it issues.
func TestAProcessFollowsAReplacementItDidNotSee(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	locator := seal.LocatorOf([]byte("message"))
	ahead := start.Add(3 * time.Hour)
	shareEpoch := epoch.Minute(ahead) + 60
	serving := open(t, directory, ahead)
	early, _, err := serving.Issue(shareEpoch, locator, ahead)
	if err != nil {
		t.Fatal(err)
	}
	replaced := 0
	sweeping, _, err := epoch.Open(directory, start, epoch.Options{Replaced: func() { replaced++ }})
	if err != nil || replaced != 1 {
		t.Fatalf("an open with the clock behind replaced %d generations: %v", replaced, err)
	}
	sweeping.Close()
	if issued, _, issueErr := serving.Issue(shareEpoch, locator, ahead); issueErr != nil || issued == early {
		t.Fatalf("a process issued from a generation another one had replaced: %v", issueErr)
	}
}

// A missing directory may be a volume that failed to mount, so only an empty one gets a new ratchet.
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
	if err != nil || !created || slices.Contains(ratchet.Generations(), first.Generations()[0]) {
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

// The seed opens every message that expires later, so it gets the protection of the pepper.
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
		{"a seed that is a directory", func(t *testing.T, directory string) string {
			t.Helper()
			path := filepath.Join(directory, epoch.FileName)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			return directory
		}, epoch.ErrNotRegular},
		{"a directory that is a file", func(t *testing.T, directory string) string {
			t.Helper()
			return filepath.Join(directory, epoch.FileName)
		}, epoch.ErrNotRegular},
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
	shareEpoch := epoch.Minute(start) + 1
	_, first, err := ratchet.Issue(shareEpoch, newLocator(), start)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := ratchet.Issue(shareEpoch, newLocator(), start)
	if err != nil || first == second {
		t.Fatalf("two messages got one share: %v", err)
	}
}

func newLocator() seal.Locator {
	id := core.NewID()
	return seal.LocatorOf(id[:])
}

func TestTheSeedFileHoldsTheMinuteSeedsAndIsErasedInPlace(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if paths := epoch.ErasablePaths(directory); !slices.Equal(paths, []string{directory}) {
		t.Fatalf("before the seed: %v", paths)
	}
	ratchet := open(t, directory, start)
	path := filepath.Join(directory, epoch.FileName)
	if paths := epoch.ErasablePaths(directory); !slices.Equal(paths, []string{directory, path}) {
		t.Fatalf("with the seed: %v", paths)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if seeds := epoch.MinuteSeeds(content); len(seeds) != len(ratchet.Generations()) {
		t.Fatalf("%d minute seeds for %d generations", len(seeds), len(ratchet.Generations()))
	}
	minute := epoch.Minute(start)
	if ratchet.Passed(minute) || !ratchet.Passed(minute-1) {
		t.Fatal("only an epoch before the minute of the ratchet has passed")
	}
}
