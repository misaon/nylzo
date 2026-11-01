// Package epoch keeps the forward-only ratchet of epoch keys that makes expiry cryptographic (spec section 5.6).
package epoch

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/seal"
)

const (
	SeedLen      = 32
	RatchetIDLen = 16
	FileName     = "seed"
	fileFormat   = "nylzo-e1"
	SeedOffset   = len(fileFormat) + RatchetIDLen + 8
	contentLen   = SeedOffset + SeedLen
	// FileLen fills one block, so ext4 never keeps the seed inline in its inode, whose changes pass the journal.
	FileLen     = erase.BlockLen
	hourSeconds = int64(time.Hour / time.Second)

	infoNext = "nylzo/v2/epoch/next"
	infoKey  = "nylzo/v2/epoch/key"
)

type (
	Seed      [SeedLen]byte
	RatchetID [RatchetIDLen]byte
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
)

// Hour is the epoch that contains the moment.
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
	return [core.ShareLen]byte(mac.Sum(nil))
}

func expand(seed *Seed, info string) [SeedLen]byte {
	derived, err := hkdf.Expand(sha256.New, seed[:], info, SeedLen)
	if err != nil {
		panic(err)
	}
	defer clear(derived)
	return [SeedLen]byte(derived)
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
)

// Inspect checks the directory and the seed as Open does, without creating, locking or advancing anything.
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
	content, err := readContent(file)
	defer clear(content)
	if err != nil {
		return StateNoSeed, err
	}
	_, _, seed, ok := decode(content)
	clear(seed[:])
	if !ok {
		return StateNoSeed, ErrCorrupt
	}
	return StateSeed, nil
}

// Ratchet holds the seed of the current hour only, in memory and in its file.
type Ratchet struct {
	path     string
	mutex    sync.Mutex
	id       RatchetID
	hour     uint32
	seed     Seed
	unsynced bool
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

func (r *Ratchet) ID() RatchetID {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.id
}

// Advance moves the ratchet to the current hour and overwrites the older seed in place. After a failed write it
// writes again, because a page that the kernel marked clean after the failure would read back as written.
func (r *Ratchet) Advance(now time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if Hour(now) <= r.hour && !r.unsynced {
		return nil
	}
	return r.loadLocked(now)
}

// Share recomputes E for a message of this ratchet that expires in the epoch; it never stores it.
func (r *Ratchet) Share(id RatchetID, hour uint32, locator seal.Locator, now time.Time) ([core.ShareLen]byte, error) {
	if err := r.Advance(now); err != nil {
		return [core.ShareLen]byte{}, err
	}
	r.mutex.Lock()
	seed, current, ratchetID := r.seed, r.hour, r.id
	r.mutex.Unlock()
	defer clear(seed[:])
	if id != ratchetID || hour < current {
		return [core.ShareLen]byte{}, ErrShareGone
	}
	for range hour - current {
		next := Next(&seed)
		seed = next
		clear(next[:])
	}
	key := Key(&seed)
	defer clear(key[:])
	return ShareOf(&key, locator), nil
}

func (r *Ratchet) Close() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	clear(r.seed[:])
}

func (r *Ratchet) create(now time.Time) (bool, error) {
	var id RatchetID
	var seed Seed
	rand.Read(id[:])
	rand.Read(seed[:])
	content := encode(id, Hour(now), &seed)
	defer clear(content)
	clear(seed[:])
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

// loadLocked reads the file under an exclusive lock, so processes that share it advance it one at a time. Memory
// moves to the new hour before the write, so no seed of a passed hour stays in it while the disk fails.
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
	if err != nil && !r.unsynced {
		return err
	}
	id, hour, seed, ok := decode(content)
	defer clear(seed[:])
	switch {
	case ok:
	case r.unsynced:
		id, hour, seed = r.id, r.hour, r.seed
	default:
		return ErrCorrupt
	}
	target := max(Hour(now), hour)
	for ; hour < target; hour++ {
		next := Next(&seed)
		seed = next
		clear(next[:])
	}
	r.id, r.hour, r.seed = id, hour, seed
	updated := encode(id, hour, &seed)
	defer clear(updated)
	if err = r.replace(r.path, updated); err != nil {
		r.unsynced = true
		return fmt.Errorf("epoch: overwrite the seed: %w", err)
	}
	r.unsynced = false
	return nil
}

func readContent(file *os.File) ([]byte, error) {
	content := make([]byte, FileLen+1)
	count, err := file.ReadAt(content, 0)
	if count != FileLen || (err != nil && !errors.Is(err, io.EOF)) {
		clear(content)
		return nil, ErrCorrupt
	}
	return content[:FileLen], nil
}

func encode(id RatchetID, hour uint32, seed *Seed) []byte {
	content := make([]byte, 0, FileLen)
	content = append(content, fileFormat...)
	content = append(content, id[:]...)
	content = binary.BigEndian.AppendUint64(content, uint64(hour))
	content = append(content, seed[:]...)
	return content[:FileLen]
}

func decode(content []byte) (RatchetID, uint32, Seed, bool) {
	var id RatchetID
	var seed Seed
	if len(content) != FileLen || string(content[:len(fileFormat)]) != fileFormat {
		return id, 0, seed, false
	}
	offset := len(fileFormat)
	copy(id[:], content[offset:offset+RatchetIDLen])
	offset += RatchetIDLen
	hour := binary.BigEndian.Uint64(content[offset : offset+8])
	copy(seed[:], content[SeedOffset:contentLen])
	padded := true
	for _, value := range content[contentLen:] {
		padded = padded && value == 0
	}
	if !padded || hour > uint64(^uint32(0)) || id == (RatchetID{}) {
		return id, 0, seed, false
	}
	return id, uint32(hour), seed, true
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
