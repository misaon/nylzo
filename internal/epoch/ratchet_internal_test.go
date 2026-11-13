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

func hourStart(hour uint32) time.Time {
	return time.Unix(int64(hour)*int64(time.Hour/time.Second), 0).UTC()
}

// referenceKey derives the key of a minute from the seed of the generation's first hour, as spec section 5.6 writes it.
func referenceKey(root Seed, first, shareEpoch uint32) [SeedLen]byte {
	step(&root, Next, hourOf(shareEpoch)-first)
	minute := FirstMinute(&root)
	step(&minute, NextMinute, shareEpoch%minutesPerHour)
	return MinuteKey(&minute)
}

// F13: the chain of hours and the chain of minutes in each hour make the key of every minute the spec derives, from
// any later minute the generation has moved to, across hour boundaries included.
func TestAGenerationOfMinutesMakesTheKeyOfEveryLaterMinute(t *testing.T) {
	t.Parallel()
	var root Seed
	rand.Read(root[:])
	first := hourOf(Minute(internalStart))
	startMinute := Minute(internalStart)
	for _, moves := range [][2]uint32{
		{0, 0},
		{0, 1},
		{0, 39},
		{0, 40},
		{0, 41},
		{5, 61},
		{39, 39},
		{40, 100},
		{100, 159},
		{159, 160},
		{30, Span*minutesPerHour - 21},
	} {
		var started generation
		started.startMinutes(GenerationID{1}, &root, startMinute)
		started.advance(startMinute, startMinute+moves[0])
		current := startMinute + moves[0]
		shareEpoch := startMinute + moves[1]
		if !started.makes(current, shareEpoch) {
			t.Fatalf("minute %d does not make epoch %d", current, shareEpoch)
		}
		if started.keyOf(current, shareEpoch) != referenceKey(root, first, shareEpoch) {
			t.Fatalf("the key of epoch %d from minute %d differs from the spec", shareEpoch, current)
		}
	}
}

// F13: once a minute has begun, no seed in memory or in the file yields the key of a minute before it.
func TestAPassedMinuteLeavesNoSeed(t *testing.T) {
	t.Parallel()
	var root Seed
	rand.Read(root[:])
	startMinute := Minute(internalStart)
	first := hourOf(startMinute)
	for _, later := range []uint32{1, 39, 40, 41, 125} {
		var started generation
		started.startMinutes(GenerationID{1}, &root, startMinute)
		started.advance(startMinute, startMinute+later)
		if started.makes(startMinute+later, startMinute+later-1) {
			t.Fatalf("a passed minute is still made %d minutes later", later)
		}
		for passed := startMinute; passed < startMinute+later; passed++ {
			minute := root
			step(&minute, Next, hourOf(passed)-first)
			minute = FirstMinute(&minute)
			step(&minute, NextMinute, passed%minutesPerHour)
			hour := root
			step(&hour, Next, hourOf(passed)-first)
			if started.minute == minute || started.hour == hour {
				t.Fatalf("minute %d keeps the seed of passed minute %d", startMinute+later, passed)
			}
		}
	}
}

// L6: after a failed overwrite the next advance writes again, even within the same minute, and memory has already
// left the passed minute. F12: no share comes from a generation that the file does not hold yet.
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
	if ratchet.state.minute != Minute(next) || !ratchet.unsynced {
		t.Fatalf("memory stayed at minute %d after a failed overwrite", ratchet.state.minute)
	}
	if _, _, err := ratchet.Issue(Minute(next)+1, locator, next); !errors.Is(err, diskFull) {
		t.Fatalf("a share came from a generation the file does not hold: %v", err)
	}
	generation, _, err := ratchet.Issue(Minute(next)+1, locator, next)
	if err != nil || written != 3 || ratchet.unsynced {
		t.Fatalf("the issue within the minute wrote %d times: %v", written, err)
	}
	content, err := os.ReadFile(ratchet.path)
	if err != nil {
		t.Fatal(err)
	}
	var stored state
	if !decode(content, &stored) || stored != ratchet.state || stored.named(generation) == nil {
		t.Fatalf("the file holds minute %d without the generation that issued the share", stored.minute)
	}
	if err = ratchet.Advance(next.Add(30 * time.Second)); err != nil || written != 3 {
		t.Fatalf("a synced ratchet wrote again within its minute: %d %v", written, err)
	}
	if err = ratchet.Advance(next.Add(time.Minute)); err != nil || written != 4 {
		t.Fatalf("the next minute was not written: %d %v", written, err)
	}
}

// F12: the single chain of an earlier release keeps every share it handed out but issues nothing more, and a crash
// or a full disk at any point of the conversion leaves a file that reads as the old chain or the new file.
func TestALegacySeedKeepsItsSharesAndIssuesNoMore(t *testing.T) {
	t.Parallel()
	hour := hourOf(Minute(internalStart))
	locator := seal.LocatorOf([]byte("message"))
	legacyID := GenerationID{9}
	var seed Seed
	rand.Read(seed[:])
	latest := hour + Span
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
			want := legacyShare(seed, latest-hour, locator)
			if got, shareErr := ratchet.Share(legacyID, latest, locator, now); shareErr != nil || got != want {
				t.Fatalf("a share of the earlier release changed: %v", shareErr)
			}
			if _, passedErr := ratchet.Share(legacyID, hour+testCase.later-1, locator, now); testCase.later > 0 &&
				!errors.Is(passedErr, ErrShareExpired) {
				t.Fatalf("the share of a passed hour: %v", passedErr)
			}
			if issuer, _, issueErr := ratchet.Issue(Minute(now)+60, locator, now); issueErr != nil ||
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
			if len(content) != FileLen || !os.SameFile(before, after) || !zeroed(content[512:]) ||
				(testCase.later > 0 && bytes.Contains(content, seed[:])) {
				t.Fatal("the conversion replaced the file, wrote past its first sector or left the passed seed in it")
			}
			if err = ratchet.Advance(hourStart(latest + 1)); err != nil ||
				slices.Contains(ratchet.Generations(), legacyID) {
				t.Fatalf("the legacy generation outlived its last epoch: %v", err)
			}
		})
	}
}

func legacyShare(seed Seed, hours uint32, locator seal.Locator) [32]byte {
	step(&seed, Next, hours)
	key := Key(&seed)
	return ShareOf(&key, locator)
}

// A write that a crash cuts short leaves some slots behind, here across an hour; the next load brings them forward,
// makes the same shares and overwrites the passed seeds.
func TestATornWriteKeepsEveryShare(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	ratchet := openInternal(t, directory, internalStart)
	shareEpoch := Minute(internalStart) + 5*minutesPerHour + 7
	locator := seal.LocatorOf([]byte("message"))
	first, _, err := ratchet.Issue(shareEpoch, locator, internalStart)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := ratchet.Issue(shareEpoch, locator, internalStart.Add(70*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, FileName)
	older, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	later := internalStart.Add(2*time.Hour + 9*time.Minute)
	wants := map[GenerationID][32]byte{}
	for _, id := range []GenerationID{first, second} {
		want, shareErr := ratchet.Share(id, shareEpoch, locator, later)
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
	copy(torn[2*slotLen:3*slotLen], older[2*slotLen:3*slotLen])
	tornDirectory := t.TempDir()
	if err = os.WriteFile(filepath.Join(tornDirectory, FileName), torn, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened := openInternal(t, tornDirectory, later)
	for id, want := range wants {
		if got, shareErr := reopened.Share(id, shareEpoch, locator, later); shareErr != nil || got != want {
			t.Fatalf("a slot behind made another share: %v", shareErr)
		}
	}
	rewritten, err := os.ReadFile(filepath.Join(tornDirectory, FileName))
	if err != nil || !bytes.Equal(rewritten, newer) {
		t.Fatalf("the load left the torn file as it was: %v", err)
	}
}

// A torn write can also leave the converted chain of an earlier release an hour behind; the load steps it forward by
// whole hours and it keeps making the same shares.
func TestATornWriteKeepsTheSharesOfALegacyChain(t *testing.T) {
	t.Parallel()
	hour := hourOf(Minute(internalStart))
	locator := seal.LocatorOf([]byte("message"))
	legacyID := GenerationID{9}
	var seed Seed
	rand.Read(seed[:])
	directory := t.TempDir()
	path := filepath.Join(directory, FileName)
	if err := os.WriteFile(path, legacyContent(legacyID, hour, &seed), 0o600); err != nil {
		t.Fatal(err)
	}
	ratchet := openInternal(t, directory, internalStart)
	older, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	later := internalStart.Add(time.Hour + 5*time.Minute)
	if err = ratchet.Advance(later); err != nil {
		t.Fatal(err)
	}
	newer, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	torn := bytes.Clone(newer)
	copy(torn[slotLen:2*slotLen], older[slotLen:2*slotLen])
	tornDirectory := t.TempDir()
	if err = os.WriteFile(filepath.Join(tornDirectory, FileName), torn, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened := openInternal(t, tornDirectory, later)
	latest := hour + Span
	if got, shareErr := reopened.Share(legacyID, latest, locator, later); shareErr != nil ||
		got != legacyShare(seed, latest-hour, locator) {
		t.Fatalf("a legacy slot an hour behind made another share: %v", shareErr)
	}
	rewritten, err := os.ReadFile(filepath.Join(tornDirectory, FileName))
	if err != nil || !bytes.Equal(rewritten, newer) {
		t.Fatalf("the load left the torn legacy slot as it was: %v", err)
	}
}

// decode accepts a file with every generation a ratchet can hold and refuses each damage that would otherwise make
// wrong shares.
func TestDecodeRefusesADamagedSlot(t *testing.T) {
	t.Parallel()
	var full state
	for hour := uint32(1000); hour <= 1000+Span+3; hour++ {
		full.advance(hour*minutesPerHour + 17)
	}
	legacy := &full.slots[len(full.slots)-1]
	*legacy = generation{id: GenerationID{7}, kind: kindHours, first: 1000 + Span, last: 1000 + 2*Span}
	rand.Read(legacy.hour[:])
	content := encode(&full)
	var decoded state
	if !decode(content, &decoded) || decoded != full || full.issuer() == nil {
		t.Fatal("a file with every slot in use was refused or changed")
	}
	slotOf := func(content []byte, index int) []byte { return content[(index+1)*slotLen : (index+2)*slotLen] }
	firstSlot := func(content []byte) []byte { return slotOf(content, 0) }
	legacySlot := func(content []byte) []byte { return slotOf(content, len(full.slots)-1) }
	resum := func(slot []byte) {
		binary.BigEndian.PutUint32(slot[sumOffset:], crc32.Checksum(slot[:sumOffset], castagnoli))
	}
	setField := func(slot []byte, offset int, value uint32) {
		binary.BigEndian.PutUint32(slot[offset:], value)
		resum(slot)
	}
	field := func(slot []byte, offset int) uint32 { return binary.BigEndian.Uint32(slot[offset:]) }
	for name, damage := range map[string]func(content []byte){
		"a flipped hour seed bit":   func(content []byte) { firstSlot(content)[hourSeedOffset] ^= 1 },
		"a flipped minute seed bit": func(content []byte) { firstSlot(content)[minuteSeedOffset] ^= 1 },
		"a zero id": func(content []byte) {
			slot := firstSlot(content)
			clear(slot[:GenerationIDLen])
			resum(slot)
		},
		"an unknown kind": func(content []byte) {
			slot := firstSlot(content)
			slot[kindOffset] = 3
			resum(slot)
		},
		"bytes after the kind": func(content []byte) {
			slot := firstSlot(content)
			slot[kindOffset+1] = 1
			resum(slot)
		},
		"padding before the checksum": func(content []byte) {
			slot := firstSlot(content)
			slot[sumOffset-1] = 1
			resum(slot)
		},
		"a minute before its first hour": func(content []byte) {
			slot := firstSlot(content)
			setField(slot, minuteOffset, field(slot, firstOffset)*minutesPerHour-1)
		},
		"a minute after its last epoch": func(content []byte) {
			slot := firstSlot(content)
			setField(slot, minuteOffset, field(slot, lastOffset)+1)
		},
		"a life longer than Span": func(content []byte) {
			slot := firstSlot(content)
			setField(slot, lastOffset, field(slot, lastOffset)+1)
		},
		"a chain of hours longer than its span": func(content []byte) {
			slot := legacySlot(content)
			setField(slot, lastOffset, field(slot, firstOffset)+legacySpan+1)
		},
		"a chain of hours with a minute seed": func(content []byte) {
			slot := legacySlot(content)
			slot[minuteSeedOffset] = 1
			resum(slot)
		},
		"two slots of one generation": func(content []byte) {
			copy(slotOf(content, 1), firstSlot(content))
		},
		"two generations of one hour": func(content []byte) {
			second := slotOf(content, 1)
			copy(second, firstSlot(content))
			second[0] ^= 1
			resum(second)
		},
		"no generation":  func(content []byte) { clear(content[slotLen:]) },
		"another format": func(content []byte) { content[0] ^= 1 },
	} {
		damaged := bytes.Clone(content)
		damage(damaged)
		var refused state
		if decode(damaged, &refused) || refused != (state{}) {
			t.Fatalf("decode accepted %s or kept what it read", name)
		}
	}
	development := make([]byte, 3*4096)
	copy(development, "nylzo-e2")
	copy(development[64:], firstSlot(content))
	if decode(development, &decoded) {
		t.Fatal("decode accepted the format of a development build")
	}
}

// A held generation asked for an epoch past its life is a fault, never a lost share that would end the message.
func TestAnEpochPastItsGenerationIsAFault(t *testing.T) {
	t.Parallel()
	ratchet := openInternal(t, t.TempDir(), internalStart)
	locator := seal.LocatorOf([]byte("message"))
	generation, _, err := ratchet.Issue(Minute(internalStart)+1, locator, internalStart)
	if err != nil {
		t.Fatal(err)
	}
	beyond := (hourOf(Minute(internalStart))+Span)*minutesPerHour + 5
	if _, err = ratchet.Share(generation, beyond, locator, internalStart); !errors.Is(err, errBeyondGeneration) ||
		errors.Is(err, ErrShareGone) {
		t.Fatalf("an epoch past the generation: %v", err)
	}
}

// Close erases the seeds; a share asked for afterwards is an error, never a lost share that would end the message.
func TestAClosedRatchetLosesNoShare(t *testing.T) {
	t.Parallel()
	ratchet := openInternal(t, t.TempDir(), internalStart)
	locator := seal.LocatorOf([]byte("message"))
	shareEpoch := Minute(internalStart) + 1
	generation, _, err := ratchet.Issue(shareEpoch, locator, internalStart)
	if err != nil {
		t.Fatal(err)
	}
	ratchet.Close()
	if _, err = ratchet.Share(generation, shareEpoch, locator, internalStart); !errors.Is(err, errClosed) {
		t.Fatalf("a closed ratchet answered %v", err)
	}
}

// After a backward clock step the ratchet issues nothing until the clock catches up, also when the step follows a
// failed write that left the file a minute behind memory.
func TestABackwardClockStepIssuesNothing(t *testing.T) {
	t.Parallel()
	locator := seal.LocatorOf([]byte("message"))
	shareEpoch := Minute(internalStart) + 5*minutesPerHour
	ratchet := openInternal(t, t.TempDir(), internalStart.Add(2*time.Minute))
	if _, _, err := ratchet.Issue(shareEpoch, locator, internalStart); !errors.Is(err, ErrClockBehind) {
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
	if err := failing.Advance(internalStart.Add(2 * time.Minute)); err == nil {
		t.Fatal("the failing write succeeded")
	}
	if _, _, err := failing.Issue(shareEpoch, locator, internalStart); !errors.Is(err, ErrClockBehind) ||
		written != 2 || failing.state.minute != Minute(internalStart)+2 {
		t.Fatalf("the retry after a clock step went back to minute %d: %v", failing.state.minute, err)
	}
}

// A request that read the clock just before the hour ended is still issued after another request moved the ratchet
// into the next hour, from the generation of the new hour.
func TestARequestFromTheEndOfTheHourIsIssued(t *testing.T) {
	t.Parallel()
	boundary := hourStart(hourOf(Minute(internalStart)) + 1)
	ratchet := openInternal(t, t.TempDir(), internalStart)
	locator := seal.LocatorOf([]byte("message"))
	shareEpoch := Of(boundary.Add(time.Minute))
	moved, _, err := ratchet.Issue(shareEpoch, locator, boundary.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	late, _, err := ratchet.Issue(shareEpoch, locator, boundary.Add(-time.Millisecond))
	if err != nil || late != moved {
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
