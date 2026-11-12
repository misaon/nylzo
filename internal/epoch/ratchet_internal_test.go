package epoch

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

var internalStart = time.Date(2026, 11, 1, 10, 20, 0, 0, time.UTC)

func openInternal(t *testing.T, directory string, now time.Time) *Ratchet {
	t.Helper()
	ratchet, _, err := Open(directory, now, Options{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ratchet.Close)
	return ratchet
}

// L6: after a failed overwrite the next advance writes again, even within the same hour, and memory has already left
// the passed hour. F12: no share comes from a generation that the file does not hold yet.
func TestAFailedOverwriteIsWrittenAgainBeforeAShareIsIssued(t *testing.T) {
	t.Parallel()
	ratchet := openInternal(t, t.TempDir(), internalStart)
	diskFull := errors.New("no space left on device")
	written := 0
	ratchet.replace = func(path string, content []byte) error {
		written++
		if written <= 2 {
			return diskFull
		}
		return os.WriteFile(path, content, 0o600)
	}
	next := internalStart.Add(time.Hour)
	locator := seal.LocatorOf([]byte("message"))
	if err := ratchet.Advance(next); !errors.Is(err, diskFull) {
		t.Fatalf("the failed overwrite: %v", err)
	}
	if ratchet.state.hour != Hour(next) || !ratchet.unsynced {
		t.Fatalf("memory stayed at hour %d after a failed overwrite", ratchet.state.hour)
	}
	if _, _, err := ratchet.Issue(Hour(next)+1, locator, next); !errors.Is(err, diskFull) {
		t.Fatalf("a share came from a generation the file does not hold: %v", err)
	}
	generation, _, err := ratchet.Issue(Hour(next)+1, locator, next)
	if err != nil || written != 3 || ratchet.unsynced {
		t.Fatalf("the issue within the hour wrote %d times: %v", written, err)
	}
	content, err := os.ReadFile(ratchet.path)
	if err != nil {
		t.Fatal(err)
	}
	var stored state
	if !decode(content, &stored) || stored != ratchet.state || stored.named(generation) == nil {
		t.Fatalf("the file holds hour %d without the generation that issued the share", stored.hour)
	}
	if err = ratchet.Advance(next.Add(time.Minute)); err != nil || written != 3 {
		t.Fatalf("a synced ratchet wrote again within its hour: %d %v", written, err)
	}
}

// F12: the single chain of an earlier release keeps every share it handed out but issues nothing more, and a crash
// or a full disk at any point of the conversion leaves a file that reads as the old chain or the new file.
func TestALegacySeedKeepsItsSharesAndIssuesNoMore(t *testing.T) {
	t.Parallel()
	hour := Hour(internalStart)
	locator := seal.LocatorOf([]byte("message"))
	legacyID := GenerationID{9}
	var seed Seed
	rand.Read(seed[:])
	latest := hour + Span - 1
	legacyFile := legacyContent(legacyID, hour, &seed)
	grown := append(bytes.Clone(legacyFile), make([]byte, FileLen-legacyLen)...)
	halfGrown := grown[:legacyLen+3*512]
	for name, testCase := range map[string]struct {
		content []byte
		later   uint32
	}{
		"in its hour":           {legacyFile, 0},
		"three hours later":     {legacyFile, 3},
		"grown by a crash":      {grown, 0},
		"half grown, disk full": {halfGrown, 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			path := filepath.Join(directory, FileName)
			if err := os.WriteFile(path, testCase.content, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if state, inspectErr := Inspect(directory); state != StateLegacySeed || inspectErr != nil {
				t.Fatalf("inspecting a legacy seed: %v %v", state, inspectErr)
			}
			now := internalStart.Add(time.Duration(testCase.later) * time.Hour)
			ratchet, _, err := Open(directory, now, Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ratchet.Close)
			copied := seed
			want := shareFrom(&copied, latest-hour, locator)
			if got, shareErr := ratchet.Share(legacyID, latest, locator, now); shareErr != nil || got != want {
				t.Fatalf("a share of the earlier release changed: %v", shareErr)
			}
			if issuer, _, issueErr := ratchet.Issue(hour+testCase.later+1, locator, now); issueErr != nil ||
				issuer == legacyID {
				t.Fatalf("the old chain issued a new share: %v", issueErr)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(content) != FileLen || !os.SameFile(before, after) ||
				(testCase.later > 0 && bytes.Contains(content, seed[:])) {
				t.Fatal("the conversion replaced the file or left the passed seed in it")
			}
			if err = ratchet.Advance(End(latest)); err != nil || slices.Contains(ratchet.Generations(), legacyID) {
				t.Fatalf("the legacy generation outlived its last epoch: %v", err)
			}
		})
	}
}

// A write that a crash cuts short leaves some slots an hour behind; the next load brings them forward, makes the same
// shares and overwrites the passed seeds.
func TestATornWriteKeepsEveryShare(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	ratchet := openInternal(t, directory, internalStart)
	hour := Hour(internalStart)
	locator := seal.LocatorOf([]byte("message"))
	first, _, err := ratchet.Issue(hour+5, locator, internalStart)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := ratchet.Issue(hour+5, locator, internalStart.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, FileName)
	older, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	later := internalStart.Add(2 * time.Hour)
	wants := map[GenerationID][32]byte{}
	for _, id := range []GenerationID{first, second} {
		want, shareErr := ratchet.Share(id, hour+5, locator, later)
		if shareErr != nil {
			t.Fatal(shareErr)
		}
		wants[id] = want
	}
	newer, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	torn := bytes.Clone(newer)
	secondSlot := torn[2*slotLen : 3*slotLen]
	copy(secondSlot, older[2*slotLen:3*slotLen])
	tornDirectory := t.TempDir()
	if err = os.WriteFile(filepath.Join(tornDirectory, FileName), torn, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened := openInternal(t, tornDirectory, later)
	for id, want := range wants {
		if got, shareErr := reopened.Share(id, hour+5, locator, later); shareErr != nil || got != want {
			t.Fatalf("a slot an hour behind made another share: %v", shareErr)
		}
	}
	rewritten, err := os.ReadFile(filepath.Join(tornDirectory, FileName))
	if err != nil || !bytes.Equal(rewritten, newer) {
		t.Fatalf("the load left the torn file as it was: %v", err)
	}
}

// decode accepts a file with every slot in use and refuses each damage that would otherwise make wrong shares.
func TestDecodeRefusesADamagedSlot(t *testing.T) {
	t.Parallel()
	var full state
	for hour := uint32(1000); hour <= 1000+Span+3; hour++ {
		full.advance(hour)
	}
	content := encode(&full)
	var decoded state
	if !decode(content, &decoded) || decoded != full || full.issuer() == nil {
		t.Fatal("a file with every slot in use was refused or changed")
	}
	firstSlot := func(content []byte) []byte { return content[slotLen : 2*slotLen] }
	resum := func(slot []byte) {
		binary.BigEndian.PutUint32(slot[sumOffset:], crc32.Checksum(slot[:sumOffset], castagnoli))
	}
	for name, damage := range map[string]func(content []byte){
		"a flipped seed bit": func(content []byte) { firstSlot(content)[seedOffset] ^= 1 },
		"a zero id": func(content []byte) {
			slot := firstSlot(content)
			clear(slot[:GenerationIDLen])
			resum(slot)
		},
		"a seed hour before its start": func(content []byte) {
			slot := firstSlot(content)
			binary.BigEndian.PutUint32(slot[hourOffset:], binary.BigEndian.Uint32(slot[firstOffset:])-1)
			resum(slot)
		},
		"a span longer than Span": func(content []byte) {
			slot := firstSlot(content)
			binary.BigEndian.PutUint32(slot[lastOffset:], binary.BigEndian.Uint32(slot[lastOffset:])+1)
			resum(slot)
		},
		"two slots of one generation": func(content []byte) {
			copy(content[2*slotLen:3*slotLen], firstSlot(content))
		},
		"bytes after the slots": func(content []byte) { content[FileLen-1] = 1 },
		"no generation":         func(content []byte) { clear(content[slotLen:]) },
		"another format":        func(content []byte) { content[0] ^= 1 },
	} {
		damaged := bytes.Clone(content)
		damage(damaged)
		var refused state
		if decode(damaged, &refused) || refused != (state{}) {
			t.Fatalf("decode accepted %s or kept what it read", name)
		}
	}
}

// Close erases the seeds; a share asked for afterwards is an error, never a lost share that would end the message.
func TestAClosedRatchetLosesNoShare(t *testing.T) {
	t.Parallel()
	ratchet := openInternal(t, t.TempDir(), internalStart)
	locator := seal.LocatorOf([]byte("message"))
	generation, _, err := ratchet.Issue(Hour(internalStart)+1, locator, internalStart)
	if err != nil {
		t.Fatal(err)
	}
	ratchet.Close()
	if _, err = ratchet.Share(generation, Hour(internalStart)+1, locator, internalStart); !errors.Is(err, errClosed) {
		t.Fatalf("a closed ratchet answered %v", err)
	}
}

// After a backward clock step the generation of the later hour issues nothing until the clock catches up, also when
// the step follows a failed write that left the file an hour behind memory.
func TestABackwardClockStepIssuesNothing(t *testing.T) {
	t.Parallel()
	locator := seal.LocatorOf([]byte("message"))
	ratchet := openInternal(t, t.TempDir(), internalStart.Add(2*time.Hour))
	if _, _, err := ratchet.Issue(Hour(internalStart)+5, locator, internalStart); !errors.Is(err, errNoIssuer) {
		t.Fatalf("a clock behind the ratchet issued a share: %v", err)
	}
	failing := openInternal(t, t.TempDir(), internalStart)
	written := 0
	failing.replace = func(path string, content []byte) error {
		written++
		if written == 1 {
			return errors.New("disk full")
		}
		return os.WriteFile(path, content, 0o600)
	}
	if err := failing.Advance(internalStart.Add(time.Hour)); err == nil {
		t.Fatal("the failing write succeeded")
	}
	if _, _, err := failing.Issue(Hour(internalStart)+5, locator, internalStart); !errors.Is(err, errNoIssuer) ||
		written != 2 || failing.state.hour != Hour(internalStart)+1 {
		t.Fatalf("the retry after a clock step went back to hour %d: %v", failing.state.hour, err)
	}
}

// A request that read the clock just before the hour ended is still issued after another request moved the ratchet
// into the next hour.
func TestARequestFromTheEndOfTheHourIsIssued(t *testing.T) {
	t.Parallel()
	boundary := End(Hour(internalStart))
	ratchet := openInternal(t, t.TempDir(), internalStart)
	locator := seal.LocatorOf([]byte("message"))
	if _, _, err := ratchet.Issue(Hour(boundary)+3, locator, boundary.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ratchet.Issue(Hour(boundary)+3, locator, boundary.Add(-time.Millisecond)); err != nil {
		t.Fatalf("a request from the end of the hour was refused: %v", err)
	}
}

func legacyContent(id GenerationID, hour uint32, seed *Seed) []byte {
	content := make([]byte, 0, legacyLen)
	content = append(content, legacyFormat...)
	content = append(content, id[:]...)
	content = binary.BigEndian.AppendUint64(content, uint64(hour))
	content = append(content, seed[:]...)
	return content[:legacyLen]
}
