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
	// Span is how many hours past the hour that starts it a generation makes shares: the longest TTL of seven
	// days, the time an upload may take and the hour by which a converted chain of an earlier release starts early.
	Span     = 7*24 + 2
	FileName = "seed"
	// FileLen holds a slot for every generation in whole blocks, so ext4 never keeps the seed inline in its inode,
	// whose changes pass the journal.
	FileLen = 3 * erase.BlockLen

	fileFormat = "nylzo-e2"
	// slotLen divides a 512-byte sector, so a write that a crash cuts short leaves every slot old or new.
	slotLen     = 64
	slots       = Span + 1
	hourLen     = 4
	firstOffset = GenerationIDLen
	lastOffset  = firstOffset + hourLen
	hourOffset  = lastOffset + hourLen
	seedOffset  = hourOffset + hourLen
	sumOffset   = seedOffset + SeedLen
	hourSeconds = int64(time.Hour / time.Second)
	// issueLag lets a request that read the clock just before the ratchet entered the next hour still be issued.
	issueLag = time.Minute

	legacyFormat     = "nylzo-e1"
	legacyLen        = erase.BlockLen
	legacyHourLen    = 8
	legacySeedOffset = len(legacyFormat) + GenerationIDLen + legacyHourLen

	infoNext = "nylzo/v2/epoch/next"
	infoKey  = "nylzo/v2/epoch/key"
)

// The header and every slot fit the file, and a slot ends with its checksum.
const (
	_ = uint(FileLen - (slots+1)*slotLen)
	_ = uint(slotLen - sumOffset - crc32.Size)
)

type (
	Seed         [SeedLen]byte
	GenerationID [GenerationIDLen]byte
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

	errNoIssuer = errors.New("epoch: no generation of this hour makes a share for the epoch")
	errClosed   = errors.New("epoch: the ratchet is closed")
	castagnoli  = crc32.MakeTable(crc32.Castagnoli)
)

func Hour(moment time.Time) uint32 {
	hours := moment.Unix() / hourSeconds
	return uint32(hours) //nolint:gosec // G115: 32 bits of hours since 1970 last until the year 491970
}

// End is the first instant after the epoch.
func End(hour uint32) time.Time {
	return time.Unix((int64(hour)+1)*hourSeconds, 0).UTC()
}

func Next(seed *Seed) Seed {
	return Seed(expand(seed, infoNext))
}

func Key(seed *Seed) [SeedLen]byte {
	return expand(seed, infoKey)
}

func ShareOf(epochKey *[SeedLen]byte, locator seal.Locator) [core.ShareLen]byte {
	mac := hmac.New(sha256.New, epochKey[:])
	mac.Write(locator[:])
	var share [core.ShareLen]byte
	mac.Sum(share[:0])
	return share
}

func advance(seed *Seed, hours uint32) {
	for range hours {
		next := Next(seed)
		*seed = next
		clear(next[:])
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
// in that hour, and goes once the hour passes its last epoch. A copy of one predicts no other.
type generation struct {
	id    GenerationID
	first uint32
	last  uint32
	seed  Seed
}

func (g *generation) used() bool {
	return g.id != GenerationID{}
}

// state holds the seeds of one hour. A generation keeps its slot for its whole life, so a rewrite never moves a seed
// to another place in the file.
type state struct {
	hour  uint32
	slots [slots]generation
}

// advance moves every generation to the hour, drops those whose last epoch has passed and starts the generation of
// the hour from fresh entropy in a free slot.
func (s *state) advance(target uint32) {
	for i := range s.slots {
		slot := &s.slots[i]
		switch {
		case !slot.used():
		case slot.last < target:
			*slot = generation{}
		default:
			advance(&slot.seed, target-s.hour)
		}
	}
	s.hour = target
	if s.issuer() != nil {
		return
	}
	for i := range s.slots {
		if fresh := &s.slots[i]; !fresh.used() {
			fresh.first, fresh.last = target, target+Span
			rand.Read(fresh.id[:])
			rand.Read(fresh.seed[:])
			return
		}
	}
}

// issuer is the generation that makes the shares of the ids issued in the hour.
func (s *state) issuer() *generation {
	for i := range s.slots {
		if s.slots[i].used() && s.slots[i].first == s.hour {
			return &s.slots[i]
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

func (s *state) makes(found *generation, shareEpoch uint32) bool {
	return s.hour <= shareEpoch && shareEpoch <= found.last
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

// Seeds lists the seeds a seed file holds, so a test can look for copies of them elsewhere.
func Seeds(content []byte) []Seed {
	var loaded state
	defer clear(loaded.slots[:])
	decode(content, &loaded)
	var seeds []Seed
	for i := range loaded.slots {
		if loaded.slots[i].used() {
			seeds = append(seeds, loaded.slots[i].seed)
		}
	}
	return seeds
}

// Ratchet holds the seeds of the current hour only, in memory and in its file.
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

// Advance moves the ratchet to the current hour and overwrites the older seeds in place. After a failed write it
// writes again, because a page that the kernel marked clean after the failure would read back as written.
func (r *Ratchet) Advance(now time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.advanceLocked(now)
}

// Issue makes the share of a new id that expires in the epoch, from the generation of the current hour, and names
// that generation. It holds the lock from the write to the share, so it never issues from a generation that only
// memory holds, and it issues nothing while the clock is behind the ratchet, so after a backward clock step one
// generation does not issue for several hours.
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
	issuer := r.state.issuer()
	if issuer == nil || Hour(now.Add(issueLag)) < r.state.hour || !r.state.makes(issuer, shareEpoch) {
		r.mutex.Unlock()
		return GenerationID{}, [core.ShareLen]byte{}, errNoIssuer
	}
	id, seed, current := issuer.id, issuer.seed, r.state.hour
	r.mutex.Unlock()
	return id, shareFrom(&seed, shareEpoch-current, locator), nil
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
	if err != nil || named == nil || !r.state.makes(named, shareEpoch) {
		r.mutex.Unlock()
		return [core.ShareLen]byte{}, cmp.Or(err, ErrShareGone)
	}
	seed, current := named.seed, r.state.hour
	r.mutex.Unlock()
	return shareFrom(&seed, shareEpoch-current, locator), nil
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
	return named != nil && r.state.makes(named, shareEpoch)
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
	if Hour(now) <= r.state.hour && !r.unsynced {
		return nil
	}
	return r.loadLocked(now)
}

// findLocked reads the file again before it gives up on a generation, because another process that shares the file
// may have started it in an hour this process has not reached yet.
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

func shareFrom(seed *Seed, hours uint32, locator seal.Locator) [core.ShareLen]byte {
	defer clear(seed[:])
	advance(seed, hours)
	key := Key(seed)
	defer clear(key[:])
	return ShareOf(&key, locator)
}

// create writes a file with the generation of the hour, so a file without any generation is always damaged.
func (r *Ratchet) create(now time.Time) (bool, error) {
	var fresh state
	defer clear(fresh.slots[:])
	fresh.advance(Hour(now))
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
// generations that another process wrote. Memory moves to the new hour before the write, so no seed of a passed hour
// stays in it while the disk fails, and its hour never goes back.
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
	loaded.advance(max(Hour(now), loaded.hour, r.state.hour))
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
		binary.BigEndian.PutUint32(slot[firstOffset:], entry.first)
		binary.BigEndian.PutUint32(slot[lastOffset:], entry.last)
		binary.BigEndian.PutUint32(slot[hourOffset:], saved.hour)
		copy(slot[seedOffset:], entry.seed[:])
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

// decodeSlots accepts slots of different hours, which a write that a crash cut short leaves behind, and brings the
// older ones forward to the newest; every slot must pass its checksum.
func decodeSlots(content []byte, into *state) bool {
	if !zeroed(content[len(fileFormat):slotLen]) || !zeroed(content[(slots+1)*slotLen:]) {
		return false
	}
	var hours [slots]uint32
	for i := range into.slots {
		slot := content[(i+1)*slotLen : (i+2)*slotLen]
		if zeroed(slot) {
			continue
		}
		hour, ok := readSlot(slot, &into.slots[i])
		if !ok {
			return false
		}
		hours[i] = hour
		into.hour = max(into.hour, hour)
	}
	if !distinct(into) || into.empty() {
		return false
	}
	for i := range into.slots {
		entry := &into.slots[i]
		switch {
		case !entry.used():
		case entry.last < into.hour:
			*entry = generation{}
		default:
			advance(&entry.seed, into.hour-hours[i])
		}
	}
	return true
}

// readSlot reads a slot that is not empty and returns the hour of its seed.
func readSlot(slot []byte, entry *generation) (uint32, bool) {
	copy(entry.id[:], slot)
	entry.first = binary.BigEndian.Uint32(slot[firstOffset:])
	entry.last = binary.BigEndian.Uint32(slot[lastOffset:])
	hour := binary.BigEndian.Uint32(slot[hourOffset:])
	copy(entry.seed[:], slot[seedOffset:sumOffset])
	valid := binary.BigEndian.Uint32(slot[sumOffset:]) == crc32.Checksum(slot[:sumOffset], castagnoli) &&
		entry.used() && entry.first <= hour && hour <= entry.last && entry.last-entry.first <= Span
	return hour, valid
}

// distinct refuses two slots of one generation or of one hour.
func distinct(loaded *state) bool {
	for i := range loaded.slots {
		for j := i + 1; j < len(loaded.slots); j++ {
			a, b := &loaded.slots[i], &loaded.slots[j]
			if a.used() && b.used() && (a.id == b.id || a.first == b.first) {
				return false
			}
		}
	}
	return true
}

// decodeLegacy turns the single chain of a release before generations into a generation that makes every share that
// release can have handed out, up to its hour plus 169, but starts in the hour before, so it never issues again and a
// copy of the old file predicts no share issued after the upgrade. Zero bytes may follow it up to the length of this
// release.
func decodeLegacy(content []byte, into *state) bool {
	entry := &into.slots[0]
	copy(entry.id[:], content[len(legacyFormat):])
	hour := binary.BigEndian.Uint64(content[len(legacyFormat)+GenerationIDLen:])
	copy(entry.seed[:], content[legacySeedOffset:])
	if hour == 0 || hour > uint64(^uint32(0)-Span) || !entry.used() || !zeroed(content[legacySeedOffset+SeedLen:]) {
		return false
	}
	into.hour = uint32(hour)
	entry.first = into.hour - 1
	entry.last = entry.first + Span
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
