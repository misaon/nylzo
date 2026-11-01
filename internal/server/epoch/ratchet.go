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
	fileLen      = len(fileFormat) + RatchetIDLen + 8 + SeedLen
	hourSeconds  = int64(time.Hour / time.Second)

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
	ErrShareGone = errors.New("epoch: the share of this message no longer exists")
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
	// Create starts a new ratchet when the directory holds none; sweep-once never does.
	Create bool
	// CheckFileSystem refuses a directory whose file system may keep an overwritten seed.
	CheckFileSystem func(directory string) error
}

// Ratchet holds the seed of the current hour only, in memory and in its file.
type Ratchet struct {
	path  string
	mutex sync.Mutex
	id    RatchetID
	hour  uint32
	seed  Seed
}

// Open loads the ratchet from the directory and advances it; it reports created when it started a new one.
func Open(directory string, now time.Time, options Options) (*Ratchet, bool, error) {
	ratchet := &Ratchet{path: filepath.Join(directory, FileName)}
	if _, statErr := os.Lstat(ratchet.path); errors.Is(statErr, fs.ErrNotExist) && !options.Create {
		return nil, false, nil
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, false, err
	}
	if options.CheckFileSystem != nil {
		if err := options.CheckFileSystem(directory); err != nil {
			return nil, false, err
		}
	}
	created := false
	if options.Create {
		var err error
		if created, err = ratchet.create(now); err != nil {
			return nil, false, err
		}
	}
	err := ratchet.load(now)
	if errors.Is(err, fs.ErrNotExist) && !options.Create {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return ratchet, created, nil
}

func (r *Ratchet) ID() RatchetID {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.id
}

// Advance moves the ratchet to the current hour and overwrites the older seed in place.
func (r *Ratchet) Advance(now time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if Hour(now) <= r.hour {
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

// loadLocked reads the file under an exclusive lock, so processes that share it advance it one at a time.
func (r *Ratchet) loadLocked(now time.Time) error {
	file, err := os.OpenFile(r.path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if lockErr := lock(file); lockErr != nil {
		return lockErr
	}
	defer unlock(file)
	content := make([]byte, fileLen+1)
	defer clear(content)
	count, err := file.ReadAt(content, 0)
	if count != fileLen || (err != nil && !errors.Is(err, io.EOF)) {
		return ErrCorrupt
	}
	id, hour, seed, ok := decode(content[:fileLen])
	defer clear(seed[:])
	if !ok {
		return ErrCorrupt
	}
	target := max(Hour(now), hour)
	for ; hour < target; hour++ {
		next := Next(&seed)
		seed = next
		clear(next[:])
	}
	if !equalHeader(content[:fileLen], id, hour) {
		updated := encode(id, hour, &seed)
		defer clear(updated)
		if err = erase.Replace(r.path, updated); err != nil {
			return fmt.Errorf("epoch: overwrite the seed: %w", err)
		}
	}
	r.id, r.hour, r.seed = id, hour, seed
	return nil
}

func encode(id RatchetID, hour uint32, seed *Seed) []byte {
	content := make([]byte, 0, fileLen)
	content = append(content, fileFormat...)
	content = append(content, id[:]...)
	content = binary.BigEndian.AppendUint64(content, uint64(hour))
	return append(content, seed[:]...)
}

func decode(content []byte) (RatchetID, uint32, Seed, bool) {
	var id RatchetID
	var seed Seed
	if len(content) != fileLen || string(content[:len(fileFormat)]) != fileFormat {
		return id, 0, seed, false
	}
	offset := len(fileFormat)
	copy(id[:], content[offset:offset+RatchetIDLen])
	offset += RatchetIDLen
	hour := binary.BigEndian.Uint64(content[offset : offset+8])
	offset += 8
	copy(seed[:], content[offset:])
	if hour > uint64(^uint32(0)) || id == (RatchetID{}) {
		return id, 0, seed, false
	}
	return id, uint32(hour), seed, true
}

func equalHeader(content []byte, id RatchetID, hour uint32) bool {
	stored, storedHour, seed, ok := decode(content)
	clear(seed[:])
	return ok && stored == id && storedHour == hour
}
