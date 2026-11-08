package disk

import (
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/iolimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	Directory          = "blobs"
	temporaryDirectory = "tmp"
	maxReferenceLen    = 64
	copyBufferLen      = 32 << 10
)

var copyBuffers = sync.Pool{New: func() any { return new([copyBufferLen]byte) }}

var ErrReference = errors.New("disk: invalid blob reference")

type Blobs struct {
	root          string
	pool          Pool
	filling       sync.Mutex
	refilling     atomic.Bool
	onFillFailure func(error)
	known         sync.Mutex
	poolNames     []string
	givenBack     map[string]struct{}
}

var _ store.Blobs = (*Blobs)(nil)

type Option func(*Blobs)

func WithPool(pool Pool) Option {
	return func(b *Blobs) { b.pool = pool }
}

// New fills the pool of empty files when it holds none, so the first upload already finds one.
func New(dataDirectory string, options ...Option) (*Blobs, error) {
	root := filepath.Join(dataDirectory, Directory)
	if err := os.MkdirAll(filepath.Join(root, temporaryDirectory), 0o700); err != nil {
		return nil, err
	}
	blobs := &Blobs{root: root, pool: defaultPool, onFillFailure: func(error) {}, givenBack: map[string]struct{}{}}
	for _, option := range options {
		option(blobs)
	}
	if err := blobs.probe(); err != nil {
		return nil, fmt.Errorf("disk: the blob directory must support hard links and directory sync: %w", err)
	}
	if err := blobs.fillEmptyPool(); err != nil {
		return nil, err
	}
	return blobs, nil
}

// PutExpiring gives the blob the blob_purge_at of a message stored with ttl at the moment the content was stored,
// so its times show nothing the row does not (spec section 5.2).
func (b *Blobs) PutExpiring(
	ctx context.Context,
	reference string,
	content io.Reader,
	size int64,
	ttl time.Duration,
) (time.Time, error) {
	return b.putStamped(ctx, reference, content, size, func(stored time.Time) time.Time {
		return store.BlobPurgeAt(store.ExpiresAt(stored, ttl))
	})
}

func (b *Blobs) Open(ctx context.Context, reference string) (io.ReadCloser, int64, error) {
	path, err := b.path(reference)
	if err != nil {
		return nil, 0, err
	}
	var file *os.File
	var info os.FileInfo
	err = withSlot(ctx, func() error {
		var openErr error
		if file, openErr = openBlob(path); openErr != nil {
			return openErr
		}
		if info, openErr = file.Stat(); openErr != nil {
			file.Close()
		}
		return openErr
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, store.ErrBlobMissing
	}
	if err != nil {
		return nil, 0, err
	}
	return &blobReader{file: file, ctx: ctx}, info.Size(), nil
}

// blobReader reads in a file slot, so the streams of every request cannot all hold a thread in a blocking read.
type blobReader struct {
	file *os.File
	ctx  context.Context //nolint:containedctx // the reader serves one request and waits for a slot under its context
}

func (r *blobReader) Read(buffer []byte) (int, error) {
	if err := iolimit.Acquire(r.ctx); err != nil {
		return 0, err
	}
	defer iolimit.Release()
	return r.file.Read(buffer)
}

func (r *blobReader) Close() error {
	return r.file.Close()
}

func (b *Blobs) Delete(ctx context.Context, reference string) error {
	if err := b.remove(reference); err != nil {
		return err
	}
	return b.syncDirectory(ctx)
}

func (b *Blobs) DeleteBatch(ctx context.Context, references []string) []error {
	errs := make([]error, len(references))
	if len(references) == 0 {
		return errs
	}
	for i, reference := range references {
		errs[i] = b.remove(reference)
	}
	if syncErr := b.syncDirectory(ctx); syncErr != nil {
		for i := range errs {
			errs[i] = cmp.Or(errs[i], syncErr)
		}
	}
	return errs
}

func (b *Blobs) List(_ context.Context) ([]store.BlobInfo, error) {
	entries, err := os.ReadDir(b.root)
	if err != nil {
		return nil, err
	}
	blobs := make([]store.BlobInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		reference, decodeErr := hex.DecodeString(entry.Name())
		if decodeErr != nil {
			continue
		}
		info, infoErr := entry.Info()
		if errors.Is(infoErr, os.ErrNotExist) {
			continue
		}
		if infoErr != nil {
			return nil, infoErr
		}
		blobs = append(blobs, store.BlobInfo{Reference: string(reference), PurgeAt: info.ModTime()})
	}
	return blobs, nil
}

// EraseAll overwrites every stored and temporary blob file in full before it unlinks it, for blobs that predate the
// erase block; a missing blob directory holds nothing to erase, and a pool file holds nothing at all.
func EraseAll(dataDirectory string) error {
	root := filepath.Join(dataDirectory, Directory)
	for _, directory := range []string{filepath.Join(root, temporaryDirectory), root} {
		if err := eraseDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func eraseDirectory(directory string) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || pooled(entry.Name()) {
			continue
		}
		if shredErr := erase.Shred(filepath.Join(directory, entry.Name()), erase.WholeFile); shredErr != nil {
			return shredErr
		}
	}
	return erase.SyncDirectory(directory)
}

// PurgeTemporary shreds the files that crashed uploads left; the pool files wait for uploads, whatever their age.
func (b *Blobs) PurgeTemporary(_ context.Context, olderThan time.Time) (int, error) {
	temporary := filepath.Join(b.root, temporaryDirectory)
	entries, err := os.ReadDir(temporary)
	if err != nil {
		return 0, err
	}
	purged := 0
	var errs []error
	for _, entry := range entries {
		if pooled(entry.Name()) {
			continue
		}
		removed, removeErr := removeStale(temporary, entry, olderThan)
		if removed {
			purged++
		}
		errs = append(errs, removeErr)
	}
	return purged, errors.Join(errs...)
}

// Restamp sets the times of every blob file and directory again, so each ctime shows the restamp, not the upload; a
// pool file keeps the ctime of the sweep that created it, unless an upload gave it back since the last restamp.
func (b *Blobs) Restamp(_ context.Context, now time.Time) error {
	givenBack := b.takeGivenBack()
	var errs []error
	for _, directory := range []string{b.root, filepath.Join(b.root, temporaryDirectory)} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, entry := range entries {
			_, returned := givenBack[entry.Name()]
			if !entry.IsDir() && (!pooled(entry.Name()) || returned) {
				errs = append(errs, restampFile(filepath.Join(directory, entry.Name()), entry))
			}
		}
	}
	quarter := now.Truncate(store.BlobPurgeStep)
	for _, directory := range []string{filepath.Join(b.root, temporaryDirectory), b.root} {
		errs = append(errs, os.Chtimes(directory, quarter, quarter))
	}
	return errors.Join(errs...)
}

func (b *Blobs) putStamped(
	ctx context.Context,
	reference string,
	content io.Reader,
	size int64,
	purgeAt func(stored time.Time) time.Time,
) (time.Time, error) {
	final, err := b.path(reference)
	if err != nil {
		return time.Time{}, err
	}
	taken, err := b.claim(ctx)
	if err != nil {
		return time.Time{}, err
	}
	stored, err := taken.write(ctx, content, size, purgeAt)
	if err == nil {
		err = withSlot(ctx, func() error { return os.Link(taken.path, final) })
	}
	if err == nil {
		removePublishedTemporary(taken.path)
	} else {
		b.giveBack(&taken)
	}
	if errors.Is(err, os.ErrExist) {
		return time.Time{}, store.ErrConflict
	}
	if err != nil {
		return time.Time{}, err
	}
	if err = b.syncDirectory(ctx); err != nil {
		return time.Time{}, errors.Join(err, erase.Shred(final, erase.BlockLen))
	}
	return stored, nil
}

// removePublishedTemporary unlinks the temporary name of a published blob, whose final name keeps the data; a name it
// cannot remove is left to PurgeTemporary.
func removePublishedTemporary(temporary string) {
	iolimit.Hold()
	os.Remove(temporary)
	iolimit.Release()
}

func (b *Blobs) probe() error {
	file, err := os.CreateTemp(filepath.Join(b.root, temporaryDirectory), "probe-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	file.Close()
	defer os.Remove(temporary)
	link := filepath.Join(b.root, temporaryDirectory, filepath.Base(temporary)+".link")
	err = os.Link(temporary, link)
	if err == nil {
		err = os.Remove(link)
	}
	if err != nil {
		return err
	}
	return erase.SyncDirectory(b.root)
}

func (b *Blobs) path(reference string) (string, error) {
	if reference == "" || len(reference) > maxReferenceLen {
		return "", ErrReference
	}
	return filepath.Join(b.root, hex.EncodeToString([]byte(reference))), nil
}

// write copies through the pooled buffer, which it clears afterwards, and stamps the file once the content is synced.
func (u *upload) write(
	ctx context.Context,
	content io.Reader,
	size int64,
	purgeAt func(stored time.Time) time.Time,
) (time.Time, error) {
	buffer, _ := copyBuffers.Get().(*[copyBufferLen]byte)
	written, err := io.CopyBuffer(slotWriter(ctx, u.file), struct{ io.Reader }{content}, buffer[:])
	clear(buffer[:])
	copyBuffers.Put(buffer)
	if err == nil && written != size {
		err = io.ErrUnexpectedEOF
	}
	if err == nil {
		err = withSlot(ctx, u.file.Sync)
	}
	if closeErr := u.file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return time.Time{}, err
	}
	stored := time.Now()
	stamp := purgeAt(stored)
	return stored, withSlot(ctx, func() error { return os.Chtimes(u.path, stamp, stamp) })
}

type writerFunc func(data []byte) (int, error)

func (write writerFunc) Write(data []byte) (int, error) {
	return write(data)
}

// slotWriter hides the file's ReadFrom, so every write goes through the copy buffer and waits for a file slot.
func slotWriter(ctx context.Context, file *os.File) io.Writer {
	return writerFunc(func(data []byte) (int, error) {
		if err := iolimit.Acquire(ctx); err != nil {
			return 0, err
		}
		defer iolimit.Release()
		return file.Write(data)
	})
}

func withSlot(ctx context.Context, work func() error) error {
	if err := iolimit.Acquire(ctx); err != nil {
		return err
	}
	defer iolimit.Release()
	return work()
}

func restampFile(path string, entry os.DirEntry) error {
	info, err := entry.Info()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	modified := info.ModTime()
	if err = os.Chtimes(path, modified, modified); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (b *Blobs) syncDirectory(ctx context.Context) error {
	return withSlot(ctx, func() error {
		directory, err := os.Open(b.root)
		if err != nil {
			return err
		}
		defer directory.Close()
		return directory.Sync()
	})
}

// remove overwrites the erase block in place before it unlinks the file, so no freed block holds its salt.
func (b *Blobs) remove(reference string) error {
	path, err := b.path(reference)
	if err != nil {
		return err
	}
	return erase.Shred(path, erase.BlockLen)
}

// removeStale only unlinks a temporary name that still shares its file with a published blob after a crash.
func removeStale(directory string, entry os.DirEntry, olderThan time.Time) (bool, error) {
	info, err := entry.Info()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.ModTime().After(olderThan) {
		return false, nil
	}
	path := filepath.Join(directory, entry.Name())
	if linkCount(info) > 1 {
		err = os.Remove(path)
	} else {
		err = erase.Shred(path, erase.BlockLen)
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func pooled(name string) bool {
	return strings.HasPrefix(name, poolPrefix)
}
