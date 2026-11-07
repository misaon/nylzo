package disk

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/iolimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	poolPrefix   = "pool-"
	uploadPrefix = "blob-"

	defaultPoolSize      = 256
	defaultPoolBatch     = 16
	minimumPoolSize      = 1024
	maximumPoolSize      = 65536
	assumedDailyCreates  = 2000
	assumedSweepInterval = time.Hour
	assumedMaxTTL        = 7 * 24 * time.Hour
	headroomDivisor      = 4
	oneDay               = 24 * time.Hour
)

// Pool sizes the empty files that uploads write into: every sweep creates Batch of them, whatever the uploads took,
// and removes random ones above Size, so neither the files' number nor their birth times follow the uploads.
type Pool struct {
	Size  int
	Batch int
}

var defaultPool = Pool{Size: defaultPoolSize, Batch: defaultPoolBatch}

var errTaken = errors.New("disk: another upload or a sweep took the pool file first")

// PoolFor creates a quarter more files at each sweep than a sweep interval's share of the daily cap, so traffic at the
// cap never empties the pool, and keeps as many as the sweeps of the longest TTL create, so a file waits that long on
// average and its birth time narrows neither the upload time nor the TTL more than the TTL range does. Without a cap it
// assumes 2000 creates a day, and with an external scheduler a sweep every hour.
func PoolFor(dailyCreates int64, sweepInterval, maxTTL time.Duration) Pool {
	if dailyCreates <= 0 {
		dailyCreates = assumedDailyCreates
	}
	if sweepInterval <= 0 {
		sweepInterval = assumedSweepInterval
	}
	if maxTTL <= 0 {
		maxTTL = assumedMaxTTL
	}
	perSweep := min(maximumPoolSize, dailyCreates/max(1, int64(oneDay/sweepInterval))+1)
	batch := min(maximumPoolSize, perSweep+(perSweep+headroomDivisor-1)/headroomDivisor)
	sweepsPerTTL := min(maximumPoolSize, max(1, int64(maxTTL/sweepInterval)))
	size := min(maximumPoolSize, max(minimumPoolSize, batch*sweepsPerTTL))
	return Pool{Size: int(size), Batch: int(batch)}
}

type upload struct {
	file           *os.File
	path           string
	pooledModified time.Time
}

// claim renames a randomly chosen pool file to an upload name before anything writes to it, so no other upload and no
// sweep in any process can take the same file. An upload that finds the pool empty refills it and is refused, so no
// stored blob ever lives in a file created for it.
func (b *Blobs) claim(ctx context.Context) (upload, error) {
	if err := iolimit.Acquire(ctx); err != nil {
		return upload{}, err
	}
	taken, err := b.takeRandom()
	iolimit.Release()
	if !errors.Is(err, store.ErrPoolEmpty) {
		return taken, err
	}
	b.filling.Lock()
	defer b.filling.Unlock()
	return upload{}, errors.Join(err, b.fillEmptyPool())
}

// takeRandom draws from the pool files this process knows and lists the directory again once per claim when it runs
// out, which also picks up files another process created.
func (b *Blobs) takeRandom() (upload, error) {
	listed := false
	for {
		name, err := b.drawPoolName()
		if err != nil {
			return upload{}, err
		}
		if name == "" && listed {
			return upload{}, store.ErrPoolEmpty
		}
		if name == "" {
			if listErr := b.listPool(); listErr != nil {
				return upload{}, listErr
			}
			listed = true
			continue
		}
		taken, err := b.take(name)
		if !errors.Is(err, errTaken) {
			return taken, err
		}
	}
}

func (b *Blobs) drawPoolName() (string, error) {
	b.known.Lock()
	defer b.known.Unlock()
	if len(b.poolNames) == 0 {
		return "", nil
	}
	index, err := randomIndex(len(b.poolNames))
	if err != nil {
		return "", err
	}
	last := len(b.poolNames) - 1
	name := b.poolNames[index]
	b.poolNames[index] = b.poolNames[last]
	b.poolNames = b.poolNames[:last]
	return name, nil
}

func (b *Blobs) listPool() error {
	names, err := b.poolFiles()
	if err != nil {
		return err
	}
	b.known.Lock()
	b.poolNames = names
	b.known.Unlock()
	return nil
}

// forgetPool makes the next claim list the directory, after this process changed the pool.
func (b *Blobs) forgetPool() {
	b.known.Lock()
	b.poolNames = nil
	b.known.Unlock()
}

// take writes the claim time into the pool file before the rename, so PurgeTemp never mistakes a fresh upload for a
// stale one; it leaves a file that is not empty or that has another link, since it would overwrite data.
func (b *Blobs) take(name string) (upload, error) {
	temporary := filepath.Join(b.root, temporaryDirectory)
	pooledPath := filepath.Join(temporary, name)
	file, err := os.OpenFile(pooledPath, os.O_WRONLY|poolOpenFlags, 0)
	if errors.Is(err, os.ErrNotExist) {
		return upload{}, errTaken
	}
	if err != nil {
		return upload{}, err
	}
	info, err := file.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() != 0 || linkCount(info) != 1) {
		err = errTaken
	}
	var pooledModified time.Time
	if err == nil {
		pooledModified = info.ModTime()
		now := time.Now()
		err = os.Chtimes(pooledPath, now, now)
	}
	uploadPath := filepath.Join(temporary, uploadPrefix+rand.Text())
	if err == nil {
		err = os.Rename(pooledPath, uploadPath)
	}
	if errors.Is(err, os.ErrNotExist) {
		err = errTaken
	}
	if err != nil {
		file.Close()
		return upload{}, err
	}
	return upload{file: file, path: uploadPath, pooledModified: pooledModified}, nil
}

// giveBack erases a failed upload and returns its file to the pool with the modification time it had there, so
// uploads that fail cannot empty the pool and the file's times show no upload once a sweep restamps it.
func (b *Blobs) giveBack(taken *upload) {
	err := erase.Overwrite(taken.path, erase.BlockLen)
	if err == nil {
		err = os.Truncate(taken.path, 0)
	}
	if err == nil {
		err = os.Chtimes(taken.path, taken.pooledModified, taken.pooledModified)
	}
	name := poolPrefix + rand.Text()
	if err == nil {
		err = os.Rename(taken.path, filepath.Join(b.root, temporaryDirectory, name))
	}
	if err != nil {
		_ = erase.Shred(taken.path, erase.BlockLen)
		return
	}
	b.known.Lock()
	b.poolNames = append(b.poolNames, name)
	b.known.Unlock()
}

// Replenish adds one batch to the pool and removes random files above its size.
func (b *Blobs) Replenish(_ context.Context) error {
	defer b.forgetPool()
	if err := b.createPoolFiles(b.pool.Batch); err != nil {
		return err
	}
	names, err := b.poolFiles()
	if err != nil {
		return err
	}
	var errs []error
	for len(names) > b.pool.Size {
		index, indexErr := randomIndex(len(names))
		if indexErr != nil {
			errs = append(errs, indexErr)
			break
		}
		errs = append(errs, b.discard(names[index]))
		names = slices.Delete(names, index, index+1)
	}
	errs = append(errs, erase.SyncDirectory(filepath.Join(b.root, temporaryDirectory)))
	return errors.Join(errs...)
}

// fillEmptyPool creates a whole pool when none is left, at startup or when an upload found it empty.
func (b *Blobs) fillEmptyPool() error {
	defer b.forgetPool()
	names, err := b.poolFiles()
	if err != nil || len(names) > 0 {
		return err
	}
	if createErr := b.createPoolFiles(b.pool.Size); createErr != nil {
		return createErr
	}
	return erase.SyncDirectory(filepath.Join(b.root, temporaryDirectory))
}

func (b *Blobs) createPoolFiles(count int) error {
	temporary := filepath.Join(b.root, temporaryDirectory)
	for range count {
		file, err := os.OpenFile(
			filepath.Join(temporary, poolPrefix+rand.Text()),
			os.O_WRONLY|os.O_CREATE|os.O_EXCL|poolOpenFlags,
			0o600,
		)
		if err != nil {
			return err
		}
		if closeErr := file.Close(); closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (b *Blobs) poolFiles() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(b.root, temporaryDirectory))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && pooled(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// discard unlinks a pool file; one that holds data is shredded, and one that shares its file with another name is only
// unlinked, so the discard never overwrites what another name keeps.
func (b *Blobs) discard(name string) error {
	path := filepath.Join(b.root, temporaryDirectory, name)
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return err
	case info.Mode().IsRegular() && info.Size() > 0 && linkCount(info) == 1:
		return erase.Shred(path, erase.WholeFile)
	}
	if err = os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func randomIndex(length int) (int, error) {
	index, err := rand.Int(rand.Reader, big.NewInt(int64(length)))
	if err != nil {
		return 0, err
	}
	return int(index.Int64()), nil
}
