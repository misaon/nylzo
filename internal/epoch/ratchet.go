// Package epoch keeps the forward-only ratchets of epoch keys that make expiry cryptographic (spec section 5.6).
package epoch

import (
	"bytes"
	"cmp"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/seal"
)

const (
	SeedLen         = 32
	GenerationIDLen = 16
	// Span is how many hours past the hour that starts it a generation of minutes makes shares: a deadline set in
	// its hour with the longest TTL of seven days falls at the latest at the start of hour first + Span.
	Span     = 7*24 + 1
	FileName = "seed"
	// FileLen holds a slot for every generation in whole blocks, so ext4 never keeps the seed inline in its inode,
	// whose changes pass the journal.
	FileLen = 6 * erase.BlockLen
	// ClockLag is how far behind the ratchet a clock may run while an id is issued: a request that read the clock just
	// before the ratchet entered the next minute is still issued.
	ClockLag = time.Minute

	fileFormat = "nylzo-e3"
	// slotLen divides a 512-byte sector, so a write that a crash cuts short leaves every slot old or new.
	slotLen          = 128
	slots            = FileLen/slotLen - 1
	fieldLen         = 4
	kindOffset       = GenerationIDLen
	firstOffset      = kindOffset + fieldLen
	lastOffset       = firstOffset + fieldLen
	minuteOffset     = lastOffset + fieldLen
	hourSeedOffset   = minuteOffset + fieldLen
	minuteSeedOffset = hourSeedOffset + SeedLen
	paddingOffset    = minuteSeedOffset + SeedLen
	sumOffset        = slotLen - crc32.Size
	minuteSeconds    = int64(time.Minute / time.Second)
	minutesPerHour   = 60

	legacyFormat     = "nylzo-e1"
	legacyLen        = erase.BlockLen
	legacyHourLen    = 8
	legacySeedOffset = len(legacyFormat) + GenerationIDLen + legacyHourLen
	// legacySpan covers every share a release before generations issued, up to its hour plus 169, from the hour
	// before it, so the converted chain never issues.
	legacySpan = Span + 1

	infoNext       = "nylzo/v2/epoch/next"
	infoKey        = "nylzo/v2/epoch/key"
	infoMinute     = "nylzo/v2/epoch/minute"
	infoMinuteNext = "nylzo/v2/epoch/minute-next"
	infoMinuteKey  = "nylzo/v2/epoch/minute-key"
)

// The header and every slot fill the file, and a slot ends with its checksum after its padding.
const (
	_ = uint((slots+1)*slotLen - FileLen)
	_ = uint(sumOffset - paddingOffset)
)

type (
	Seed         [SeedLen]byte
	GenerationID [GenerationIDLen]byte
	kind         uint8
)

const (
	// kindMinutes makes the shares of one minute each, from a chain of hours and a chain of minutes in each hour.
	kindMinutes kind = 1
	// kindHours is the chain of an earlier release, which made the shares of whole hours.
	kindHours kind = 2
)

var (
	ErrCorrupt = errors.New(
		"epoch: the seed file is damaged; move it aside to start a new ratchet, which ends every live message",
	)
	ErrNoDirectory = errors.New(
		"epoch: the epoch directory does not exist; create it with mode 0700 for the user that runs nylzo, " +
			"or mount its volume",
	)
	ErrExposed = errors.New(
		"epoch: group or others can access the epoch directory or the seed; restrict them with chmod 700 and 600",
	)
	ErrForeignOwner = errors.New("epoch: the epoch directory or the seed belongs to another user")
	ErrSymlink      = errors.New("epoch: the epoch directory or the seed is a symbolic link")
	ErrNotRegular   = errors.New("epoch: the epoch directory or the seed has the wrong file type")
	ErrShareGone    = errors.New("epoch: the share of this message no longer exists")
	// ErrShareExpired is a share that went with its deadline, while the generation that made it is still held.
	ErrShareExpired = fmt.Errorf("%w: its deadline has passed", ErrShareGone)
	// ErrClockBehind refuses an id while the clock runs behind the ratchet, which never goes back.
	ErrClockBehind = errors.New("epoch: the clock runs behind the ratchet")

	errNoIssuer = errors.New("epoch: no generation of this hour makes a share for the epoch")

	errClosed  = errors.New("epoch: the ratchet is closed")
	castagnoli = crc32.MakeTable(crc32.Castagnoli)
)

func Minute(moment time.Time) uint32 {
	minutes := moment.Unix() / minuteSeconds
	return uint32(minutes) //nolint:gosec // G115: 32 bits of minutes since 1970 last until the year 10136
}

// Of is the epoch of a message that expires at the deadline, a whole minute: the last minute in which it lives.
func Of(deadline time.Time) uint32 {
	return Minute(deadline) - 1
}

// Deadline is the instant at which the share of the epoch goes, and its message with it.
func Deadline(shareEpoch uint32) time.Time {
	return time.Unix((int64(shareEpoch)+1)*minuteSeconds, 0).UTC()
}

func hourOf(minute uint32) uint32 {
	return minute / minutesPerHour
}

// Next and Key are the chain of hours and the key of an hour that releases before minutes made shares with.
func Next(seed *Seed) Seed {
	return Seed(expand(seed, infoNext))
}

func Key(seed *Seed) [SeedLen]byte {
	return expand(seed, infoKey)
}

// FirstMinute starts the chain of minutes of the hour whose seed it is.
func FirstMinute(hourSeed *Seed) Seed {
	return Seed(expand(hourSeed, infoMinute))
}

func NextMinute(seed *Seed) Seed {
	return Seed(expand(seed, infoMinuteNext))
}

func MinuteKey(seed *Seed) [SeedLen]byte {
	return expand(seed, infoMinuteKey)
}

func ShareOf(epochKey *[SeedLen]byte, locator seal.Locator) [core.ShareLen]byte {
	mac := hmac.New(sha256.New, epochKey[:])
	mac.Write(locator[:])
	var share [core.ShareLen]byte
	mac.Sum(share[:0])
	return share
}

func step(seed *Seed, next func(*Seed) Seed, count uint32) {
	for range count {
		stepped := next(seed)
		*seed = stepped
		clear(stepped[:])
	}
}

func expand(seed *Seed, info string) [SeedLen]byte {
	derived, err := hkdf.Expand(sha256.New, seed[:], info, SeedLen)
	if err != nil {
		panic(err)
	}
	defer clear(derived)
	return [SeedLen]byte(derived)
}

// generation is a chain of seeds that starts from fresh entropy in the hour first, makes the shares of the ids issued
// in that hour, and goes once the ratchet passes its last epoch. A copy of one predicts no other. A generation of
// minutes holds the seed of the next hour and the seed of the current minute, so neither yields a passed minute; a
// chain of hours holds the seed of the current hour and makes the shares of whole hours.
type generation struct {
	id     GenerationID
	kind   kind
	first  uint32
	last   uint32
	hour   Seed
	minute Seed
}

func (g *generation) used() bool {
	return g.id != GenerationID{}
}

// startFresh draws the id and the seed of the hour from the CSPRNG, so the generation predicts no other.
func (g *generation) startFresh(minute uint32) {
	var id GenerationID
	var root Seed
	defer clear(root[:])
	rand.Read(id[:])
	rand.Read(root[:])
	g.startMinutes(id, &root, minute)
}

// startMinutes derives the seeds of the minute the generation starts in from the seed of its hour.
func (g *generation) startMinutes(id GenerationID, root *Seed, minute uint32) {
	g.id, g.kind, g.first, g.last = id, kindMinutes, hourOf(minute), (hourOf(minute)+Span)*minutesPerHour-1
	g.minute = FirstMinute(root)
	g.hour = Next(root)
	step(&g.minute, NextMinute, minute%minutesPerHour)
}

// advance moves the seeds from one minute to a later one and overwrites every seed in between.
func (g *generation) advance(from, target uint32) {
	if g.kind == kindHours {
		step(&g.hour, Next, hourOf(target)-hourOf(from))
		return
	}
	if hourOf(from) == hourOf(target) {
		step(&g.minute, NextMinute, target-from)
		return
	}
	step(&g.hour, Next, hourOf(target)-hourOf(from)-1)
	g.minute = FirstMinute(&g.hour)
	step(&g.hour, Next, 1)
	step(&g.minute, NextMinute, target%minutesPerHour)
}

// ends reports whether the generation has no share left at the minute.
func (g *generation) ends(minute uint32) bool {
	if g.kind == kindHours {
		return g.last < hourOf(minute)
	}
	return g.last < minute
}

// makes reports whether the generation, at the minute, still makes the share of the epoch: a minute, or an hour for
// a chain of hours.
func (g *generation) makes(minute, shareEpoch uint32) bool {
	if g.kind == kindHours {
		return hourOf(minute) <= shareEpoch && shareEpoch <= g.last
	}
	return minute <= shareEpoch && shareEpoch <= g.last
}

// passed reports whether the epoch ended before the minute, so its share went with its deadline.
func (g *generation) passed(minute, shareEpoch uint32) bool {
	if g.kind == kindHours {
		return shareEpoch < hourOf(minute)
	}
	return shareEpoch < minute
}

// keyOf is the epoch key of an epoch the generation makes at the minute; it moves a copy and wipes it.
func (g *generation) keyOf(minute, shareEpoch uint32) [SeedLen]byte {
	moved := *g
	defer moved.clear()
	if moved.kind == kindHours {
		step(&moved.hour, Next, shareEpoch-hourOf(minute))
		return Key(&moved.hour)
	}
	moved.advance(minute, shareEpoch)
	return MinuteKey(&moved.minute)
}

func (g *generation) clear() {
	clear(g.hour[:])
	clear(g.minute[:])
}

// state holds the seeds of one minute. A generation keeps its slot for its whole life, so a rewrite never moves a
// seed to another place in the file.
type state struct {
	minute uint32
	slots  [slots]generation
}

// advance moves every generation to the minute, drops those whose last epoch has passed and starts the generation of
// the hour from fresh entropy in a free slot.
func (s *state) advance(target uint32) {
	for i := range s.slots {
		slot := &s.slots[i]
		switch {
		case !slot.used():
		case slot.ends(target):
			*slot = generation{}
		default:
			slot.advance(s.minute, target)
		}
	}
	s.minute = target
	if s.issuer() != nil {
		return
	}
	for i := range s.slots {
		if s.slots[i].used() {
			continue
		}
		s.slots[i].startFresh(target)
		return
	}
}

// issuer is the generation that makes the shares of the ids issued in the hour.
func (s *state) issuer() *generation {
	for i := range s.slots {
		if slot := &s.slots[i]; slot.used() && slot.kind == kindMinutes && slot.first == hourOf(s.minute) {
			return slot
		}
	}
	return nil
}

// empty reports a state without any generation, which no ratchet writes: it starts with one and always keeps the
// generation of its hour.
func (s *state) empty() bool {
	for i := range s.slots {
		if s.slots[i].used() {
			return false
		}
	}
	return true
}

func (s *state) named(id GenerationID) *generation {
	for i := range s.slots {
		if s.slots[i].used() && s.slots[i].id == id {
			return &s.slots[i]
		}
	}
	return nil
}

type Options struct {
	// Create starts a new ratchet when the directory holds no seed; sweep-once never does.
	Create bool
	// CheckFileSystem refuses a directory whose file system may keep an overwritten seed.
	CheckFileSystem func(directory string) error
}

// State is what a directory holds, as a read that changes nothing finds it.
type State int

const (
	StateNoSeed State = iota
	StateSeed
	// StateLegacySeed is the single chain of a release before generations, which the next advance converts.
	StateLegacySeed
)

// Inspect checks the directory and the seed as Open does, without creating or advancing anything; it waits for a
// rewrite in progress.
func Inspect(directory string) (State, error) {
	if err := checkDirectory(directory, false); err != nil {
		return StateNoSeed, err
	}
	path := filepath.Join(directory, FileName)
	err := checkSeed(path)
	if errors.Is(err, fs.ErrNotExist) {
		return StateNoSeed, nil
	}
	if err != nil {
		return StateNoSeed, err
	}
	file, err := os.Open(path)
	if err != nil {
		return StateNoSeed, err
	}
	defer file.Close()
	if lockErr := lockShared(file); lockErr != nil {
		return StateNoSeed, lockErr
	}
	defer unlock(file)
	content, err := readContent(file)
	defer clear(content)
	if err != nil {
		return StateNoSeed, err
	}
	var loaded state
	defer clear(loaded.slots[:])
	switch {
	case !decode(content, &loaded):
		return StateNoSeed, ErrCorrupt
	case legacy(content):
		return StateLegacySeed, nil
	}
	return StateSeed, nil
}

// ErasablePaths are the directory and, once it exists, the seed file, whose overwrites must land in place.
func ErasablePaths(directory string) []string {
	paths := []string{directory}
	if seed := filepath.Join(directory, FileName); fileExists(seed) {
		paths = append(paths, seed)
	}
	return paths
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// MinuteSeeds lists the seeds of the current minute that a seed file holds, which make the share of that minute.
func MinuteSeeds(content []byte) []Seed {
	var loaded state
	defer clear(loaded.slots[:])
	decode(content, &loaded)
	var seeds []Seed
	for i := range loaded.slots {
		if slot := &loaded.slots[i]; slot.used() && slot.kind == kindMinutes {
			seeds = append(seeds, slot.minute)
		}
	}
	return seeds
}

// Seeds lists the seeds a seed file holds, so a test can look for copies of them elsewhere.
func Seeds(content []byte) []Seed {
	var loaded state
	defer clear(loaded.slots[:])
	decode(content, &loaded)
	var seeds []Seed
	for i := range loaded.slots {
		if slot := &loaded.slots[i]; slot.used() {
			seeds = append(seeds, slot.hour)
			if slot.kind == kindMinutes {
				seeds = append(seeds, slot.minute)
			}
		}
	}
	return seeds
}

// Ratchet holds the seeds of the current minute only, in memory and in its file.
type Ratchet struct {
	path     string
	mutex    sync.Mutex
	state    state
	unsynced bool
	closed   bool
	replace  func(path string, content []byte) error
}

// Open loads the ratchet from the directory and advances it; it reports created when it started a new one.
// A missing directory is refused, because a volume that failed to mount looks the same.
func Open(directory string, now time.Time, options Options) (*Ratchet, bool, error) {
	if err := checkDirectory(directory, true); err != nil {
		return nil, false, err
	}
	if options.CheckFileSystem != nil {
		if err := options.CheckFileSystem(directory); err != nil {
			return nil, false, err
		}
	}
	ratchet := &Ratchet{path: filepath.Join(directory, FileName), replace: erase.Replace}
	err := checkSeed(ratchet.path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !options.Create:
		return nil, false, nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, false, err
	}
	created := false
	if options.Create {
		if created, err = ratchet.create(now); err != nil {
			return nil, false, err
		}
	}
	if err = ratchet.load(now); err != nil {
		ratchet.Close()
		return nil, false, err
	}
	return ratchet, created, nil
}

// Generations names every generation whose shares the ratchet can still make, oldest first, so the last one issues
// in the current hour.
func (r *Ratchet) Generations() []GenerationID {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	var held []*generation
	for i := range r.state.slots {
		if r.state.slots[i].used() {
			held = append(held, &r.state.slots[i])
		}
	}
	slices.SortFunc(held, func(a, b *generation) int { return cmp.Compare(a.first, b.first) })
	ids := make([]GenerationID, 0, len(held))
	for _, found := range held {
		ids = append(ids, found.id)
	}
	return ids
}

// Advance moves the ratchet to the current minute and overwrites the older seeds in place. After a failed write it
// writes again, because a page that the kernel marked clean after the failure would read back as written.
func (r *Ratchet) Advance(now time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.advanceLocked(now)
}

// Issue makes the share of a new id whose deadline ends the epoch, from the generation of the current hour, and
// names that generation. It holds the lock from the write to the share, so it never issues from a generation that only
// memory holds, and it issues nothing while the clock is behind the ratchet, so after a backward clock step no
// generation issues outside its own hour.
func (r *Ratchet) Issue(
	shareEpoch uint32,
	locator seal.Locator,
	now time.Time,
) (GenerationID, [core.ShareLen]byte, error) {
	r.mutex.Lock()
	if err := r.advanceLocked(now); err != nil {
		r.mutex.Unlock()
		return GenerationID{}, [core.ShareLen]byte{}, err
	}
	issuer, current := r.state.issuer(), r.state.minute
	if Minute(now.Add(ClockLag)) < current {
		r.mutex.Unlock()
		return GenerationID{}, [core.ShareLen]byte{}, ErrClockBehind
	}
	if issuer == nil || !issuer.makes(current, shareEpoch) {
		r.mutex.Unlock()
		return GenerationID{}, [core.ShareLen]byte{}, errNoIssuer
	}
	found := *issuer
	r.mutex.Unlock()
	return found.id, shareFrom(&found, current, shareEpoch, locator), nil
}

// Share recomputes E for a message of the generation that expires in the epoch; it never stores it.
func (r *Ratchet) Share(
	id GenerationID,
	shareEpoch uint32,
	locator seal.Locator,
	now time.Time,
) ([core.ShareLen]byte, error) {
	r.mutex.Lock()
	named, err := r.findLocked(id, now)
	current := r.state.minute
	if err != nil || named == nil || !named.makes(current, shareEpoch) {
		passed := err == nil && named != nil && named.passed(current, shareEpoch)
		r.mutex.Unlock()
		if passed {
			return [core.ShareLen]byte{}, ErrShareExpired
		}
		return [core.ShareLen]byte{}, cmp.Or(err, ErrShareGone)
	}
	found := *named
	r.mutex.Unlock()
	return shareFrom(&found, current, shareEpoch, locator), nil
}

// Passed reports whether the minute epoch has ended by the minute of the ratchet, so its share went with its deadline
// whether or not the generation that made it is still held.
func (r *Ratchet) Passed(shareEpoch uint32) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return shareEpoch < r.state.minute
}

// Holds reports whether the ratchet still holds the generation, which a lost seed file takes along.
func (r *Ratchet) Holds(id GenerationID, now time.Time) (bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	named, err := r.findLocked(id, now)
	return named != nil, err
}

// Covers reports whether a generation that Holds found still makes the share of the epoch, so a create stores no
// message whose share nobody can make.
func (r *Ratchet) Covers(id GenerationID, shareEpoch uint32) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	named := r.state.named(id)
	return named != nil && named.makes(r.state.minute, shareEpoch)
}

// Close erases the seeds from memory; a share asked for afterwards fails instead of reading as lost.
func (r *Ratchet) Close() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.closed = true
	clear(r.state.slots[:])
}

func (r *Ratchet) advanceLocked(now time.Time) error {
	if r.closed {
		return errClosed
	}
	if Minute(now) <= r.state.minute && !r.unsynced {
		return nil
	}
	return r.loadLocked(now)
}

// findLocked reads the file again before it gives up on a generation, because another process that shares the file
// may have started it in a minute this process has not reached yet.
func (r *Ratchet) findLocked(id GenerationID, now time.Time) (*generation, error) {
	if err := r.advanceLocked(now); err != nil {
		return nil, err
	}
	if named := r.state.named(id); named != nil {
		return named, nil
	}
	if err := r.loadLocked(now); err != nil {
		return nil, err
	}
	return r.state.named(id), nil
}

func shareFrom(found *generation, current, shareEpoch uint32, locator seal.Locator) [core.ShareLen]byte {
	defer found.clear()
	key := found.keyOf(current, shareEpoch)
	defer clear(key[:])
	return ShareOf(&key, locator)
}

// create writes a file with the generation of the hour, so a file without any generation is always damaged.
func (r *Ratchet) create(now time.Time) (bool, error) {
	var fresh state
	defer clear(fresh.slots[:])
	fresh.advance(Minute(now))
	content := encode(&fresh)
	defer clear(content)
	file, err := os.OpenFile(r.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = file.Write(content)
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		return false, errors.Join(err, os.Remove(r.path))
	}
	return true, erase.SyncDirectory(filepath.Dir(r.path))
}

func (r *Ratchet) load(now time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.loadLocked(now)
}

// loadLocked reads the file under an exclusive lock, so processes that share it advance it one at a time and agree
// on the generation of each hour. It always writes the file back and syncs it, because bytes that another process
// wrote may still be only in the page cache, and it never replaces a file it cannot read from memory, which may lack
// generations that another process wrote. Memory moves to the new minute before the write, so no seed of a passed
// minute stays in it while the disk fails, and its minute never goes back.
func (r *Ratchet) loadLocked(now time.Time) error {
	file, err := os.OpenFile(r.path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if seedErr := checkSeedInfo(info); seedErr != nil {
		return seedErr
	}
	if lockErr := lock(file); lockErr != nil {
		return lockErr
	}
	defer unlock(file)
	content, err := readContent(file)
	defer clear(content)
	if err != nil {
		return err
	}
	var loaded state
	defer clear(loaded.slots[:])
	if !decode(content, &loaded) {
		return ErrCorrupt
	}
	loaded.advance(max(Minute(now), loaded.minute, r.state.minute))
	r.state = loaded
	updated := encode(&r.state)
	defer clear(updated)
	if err = r.write(legacy(content), updated); err != nil {
		r.unsynced = true
		return fmt.Errorf("epoch: overwrite the seed: %w", err)
	}
	r.unsynced = false
	return nil
}

// write pads a file of an earlier release with synced zeros before it overwrites it, every time, so a crash leaves
// the old seed in its first sector or the new one, and every length in between reads as the old file. A pad that
// fails is cut back, so a release before generations can still read the file.
func (r *Ratchet) write(converts bool, content []byte) error {
	if converts {
		if err := erase.Pad(r.path, legacyLen, int64(len(content))); err != nil {
			return errors.Join(err, erase.Cut(r.path, legacyLen))
		}
	}
	return r.replace(r.path, content)
}

// readContent reads a file of this release or one of an earlier release that a conversion may have grown. A read
// that fails says nothing about the file, so it returns that error rather than ErrCorrupt.
func readContent(file *os.File) ([]byte, error) {
	content := make([]byte, FileLen+1)
	count, err := file.ReadAt(content, 0)
	switch {
	case err != nil && !errors.Is(err, io.EOF):
		clear(content)
		return nil, err
	case count < legacyLen || count > FileLen:
		clear(content)
		return nil, ErrCorrupt
	}
	return content[:count], nil
}

func legacy(content []byte) bool {
	return bytes.HasPrefix(content, []byte(legacyFormat))
}

func encode(saved *state) []byte {
	content := make([]byte, FileLen)
	copy(content, fileFormat)
	for i := range saved.slots {
		entry := &saved.slots[i]
		if !entry.used() {
			continue
		}
		slot := content[(i+1)*slotLen : (i+2)*slotLen]
		copy(slot, entry.id[:])
		slot[kindOffset] = byte(entry.kind)
		binary.BigEndian.PutUint32(slot[firstOffset:], entry.first)
		binary.BigEndian.PutUint32(slot[lastOffset:], entry.last)
		binary.BigEndian.PutUint32(slot[minuteOffset:], saved.minute)
		copy(slot[hourSeedOffset:], entry.hour[:])
		copy(slot[minuteSeedOffset:], entry.minute[:])
		binary.BigEndian.PutUint32(slot[sumOffset:], crc32.Checksum(slot[:sumOffset], castagnoli))
	}
	return content
}

// decode fills the state from a file of this release or of an earlier one and clears it when it refuses the file.
func decode(content []byte, into *state) bool {
	var ok bool
	switch {
	case len(content) == FileLen && bytes.HasPrefix(content, []byte(fileFormat)):
		ok = decodeSlots(content, into)
	case legacy(content):
		ok = decodeLegacy(content, into)
	default:
		return false
	}
	if !ok {
		*into = state{}
	}
	return ok
}

// decodeSlots accepts slots of different minutes, which a write that a crash cut short leaves behind, and brings the
// older ones forward to the newest; every slot must pass its checksum.
func decodeSlots(content []byte, into *state) bool {
	if !zeroed(content[len(fileFormat):slotLen]) {
		return false
	}
	var minutes [slots]uint32
	for i := range into.slots {
		slot := content[(i+1)*slotLen : (i+2)*slotLen]
		if zeroed(slot) {
			continue
		}
		minute, ok := readSlot(slot, &into.slots[i])
		if !ok {
			return false
		}
		minutes[i] = minute
		into.minute = max(into.minute, minute)
	}
	if !distinct(into) || into.empty() {
		return false
	}
	for i := range into.slots {
		entry := &into.slots[i]
		switch {
		case !entry.used():
		case entry.ends(into.minute):
			*entry = generation{}
		default:
			entry.advance(minutes[i], into.minute)
		}
	}
	return true
}

// readSlot reads a slot that is not empty and returns the minute of its seeds.
func readSlot(slot []byte, entry *generation) (uint32, bool) {
	copy(entry.id[:], slot)
	entry.kind = kind(slot[kindOffset])
	entry.first = binary.BigEndian.Uint32(slot[firstOffset:])
	entry.last = binary.BigEndian.Uint32(slot[lastOffset:])
	minute := binary.BigEndian.Uint32(slot[minuteOffset:])
	copy(entry.hour[:], slot[hourSeedOffset:])
	copy(entry.minute[:], slot[minuteSeedOffset:])
	return minute, binary.BigEndian.Uint32(slot[sumOffset:]) == crc32.Checksum(slot[:sumOffset], castagnoli) &&
		entry.used() && zeroed(slot[kindOffset+1:firstOffset]) && zeroed(slot[paddingOffset:sumOffset]) &&
		entry.lives(minute) && (entry.kind == kindMinutes || entry.minute == Seed{})
}

// lives checks that the minute lies within the life of the generation and that the life fits its kind.
func (g *generation) lives(minute uint32) bool {
	switch g.kind {
	case kindMinutes:
		return g.first <= hourOf(minute) && minute <= g.last && g.last < (g.first+Span)*minutesPerHour
	case kindHours:
		return g.first <= hourOf(minute) && hourOf(minute) <= g.last && g.last-g.first <= legacySpan
	default:
		return false
	}
}

// distinct refuses two slots of one generation or two generations of one kind that start in one hour.
func distinct(loaded *state) bool {
	for i := range loaded.slots {
		for j := i + 1; j < len(loaded.slots); j++ {
			a, b := &loaded.slots[i], &loaded.slots[j]
			if a.used() && b.used() && (a.id == b.id || (a.kind == b.kind && a.first == b.first)) {
				return false
			}
		}
	}
	return true
}

// decodeLegacy turns the single chain of a release before generations into a chain of hours that makes every share
// that release can have handed out, up to its hour plus 169. It starts in the hour before and makes no minutes, so it
// never issues again and a copy of the old file predicts no share issued after the upgrade. Zero bytes may follow it
// up to the length of this release.
func decodeLegacy(content []byte, into *state) bool {
	entry := &into.slots[0]
	copy(entry.id[:], content[len(legacyFormat):])
	hour := binary.BigEndian.Uint64(content[len(legacyFormat)+GenerationIDLen:])
	copy(entry.hour[:], content[legacySeedOffset:])
	if hour == 0 || hour > uint64(^uint32(0)/minutesPerHour-legacySpan) || !entry.used() ||
		!zeroed(content[legacySeedOffset+SeedLen:]) {
		return false
	}
	into.minute = uint32(hour) * minutesPerHour
	entry.kind = kindHours
	entry.first = uint32(hour) - 1
	entry.last = entry.first + legacySpan
	return true
}

func zeroed(content []byte) bool {
	for _, value := range content {
		if value != 0 {
			return false
		}
	}
	return true
}

// checkDirectory narrows a directory of this user as the data directory is narrowed (D99), when it may write.
func checkDirectory(directory string, narrow bool) error {
	info, err := os.Lstat(directory)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrNoDirectory
	case err != nil:
		return err
	case info.Mode()&fs.ModeSymlink != 0:
		return ErrSymlink
	case !info.IsDir():
		return ErrNotRegular
	case !ownedByProcess(info):
		return ErrForeignOwner
	case shared(info) && narrow:
		return os.Chmod(directory, 0o700) //nolint:gosec // G302: a directory needs its search bit
	}
	return nil
}

// checkSeed refuses what the pepper file refuses: a link, another owner, and access for group or others.
func checkSeed(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return checkSeedInfo(info)
}

func checkSeedInfo(info fs.FileInfo) error {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return ErrSymlink
	case !info.Mode().IsRegular():
		return ErrNotRegular
	case !ownedByProcess(info):
		return ErrForeignOwner
	case shared(info):
		return ErrExposed
	}
	return nil
}
