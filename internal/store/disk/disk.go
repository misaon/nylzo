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
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/store"
)

const (
	Directory          = "blobs"
	temporaryDirectory = "tmp"
	maxRefLen          = 64
	copyBufferLen      = 32 << 10
)

var copyBuffers = sync.Pool{New: func() any { return new([copyBufferLen]byte) }}

var ErrRef = errors.New("disk: invalid blob reference")

type Blobs struct {
	root string
}

var _ store.Blobs = (*Blobs)(nil)

func New(dataDirectory string) (*Blobs, error) {
	root := filepath.Join(dataDirectory, Directory)
	if err := os.MkdirAll(filepath.Join(root, temporaryDirectory), 0o700); err != nil {
		return nil, err
	}
	blobs := &Blobs{root: root}
	if err := blobs.probe(); err != nil {
		return nil, fmt.Errorf("disk: the blob directory must support hard links and directory sync: %w", err)
	}
	return blobs, nil
}

func (b *Blobs) Put(_ context.Context, ref string, content io.Reader, size int64) error {
	final, err := b.path(ref)
	if err != nil {
		return err
	}
	temporary, err := b.writeTemp(content, size)
	if err == nil {
		err = os.Link(temporary, final)
	}
	if temporary != "" {
		dropTemporary(temporary, err == nil)
	}
	if errors.Is(err, os.ErrExist) {
		return store.ErrConflict
	}
	if err != nil {
		return err
	}
	if err = b.syncDirectory(); err != nil {
		return errors.Join(err, erase.Shred(final, erase.BlockLen))
	}
	return nil
}

// dropTemporary unlinks the temporary name of a published blob, whose final name keeps the data, and shreds any other;
// a temporary file it cannot drop is left to PurgeTemp.
func dropTemporary(temporary string, published bool) {
	if published {
		os.Remove(temporary)
		return
	}
	_ = erase.Shred(temporary, erase.BlockLen)
}

func (b *Blobs) Open(_ context.Context, ref string) (io.ReadCloser, int64, error) {
	path, err := b.path(ref)
	if err != nil {
		return nil, 0, err
	}
	file, err := openBlob(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, store.ErrBlobMissing
	}
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}
	return file, info.Size(), nil
}

func (b *Blobs) Delete(_ context.Context, ref string) error {
	if err := b.remove(ref); err != nil {
		return err
	}
	return b.syncDirectory()
}

func (b *Blobs) DeleteBatch(_ context.Context, refs []string) []error {
	errs := make([]error, len(refs))
	if len(refs) == 0 {
		return errs
	}
	for i, ref := range refs {
		errs[i] = b.remove(ref)
	}
	if syncErr := b.syncDirectory(); syncErr != nil {
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
		ref, decodeErr := hex.DecodeString(entry.Name())
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
		blobs = append(blobs, store.BlobInfo{Ref: string(ref), Modified: info.ModTime()})
	}
	return blobs, nil
}

func (b *Blobs) PurgeTemp(_ context.Context, olderThan time.Time) (int, error) {
	temporary := filepath.Join(b.root, temporaryDirectory)
	entries, err := os.ReadDir(temporary)
	if err != nil {
		return 0, err
	}
	purged := 0
	var errs []error
	for _, entry := range entries {
		removed, removeErr := removeStale(temporary, entry, olderThan)
		if removed {
			purged++
		}
		errs = append(errs, removeErr)
	}
	return purged, errors.Join(errs...)
}

// Restamp sets the times of every blob file and directory again, so each ctime shows the restamp, not the upload.
func (b *Blobs) Restamp(_ context.Context, now time.Time) error {
	var errs []error
	for _, directory := range []string{b.root, filepath.Join(b.root, temporaryDirectory)} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
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
	return b.syncDirectory()
}

func (b *Blobs) path(ref string) (string, error) {
	if ref == "" || len(ref) > maxRefLen {
		return "", ErrRef
	}
	return filepath.Join(b.root, hex.EncodeToString([]byte(ref))), nil
}

func (b *Blobs) writeTemp(content io.Reader, size int64) (string, error) {
	file, err := os.CreateTemp(filepath.Join(b.root, temporaryDirectory), "blob-*")
	if err != nil {
		return "", err
	}
	buffer, _ := copyBuffers.Get().(*[copyBufferLen]byte)
	written, err := io.CopyBuffer(struct{ io.Writer }{file}, content, buffer[:])
	clear(buffer[:])
	copyBuffers.Put(buffer)
	if err == nil && written != size {
		err = io.ErrUnexpectedEOF
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		quarter := store.CeilQuarterHour(time.Now())
		err = os.Chtimes(file.Name(), quarter, quarter)
	}
	return file.Name(), err
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

func (b *Blobs) syncDirectory() error {
	directory, err := os.Open(b.root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// remove overwrites the erase block in place before it unlinks the file, so no freed block holds its salt.
func (b *Blobs) remove(ref string) error {
	path, err := b.path(ref)
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
