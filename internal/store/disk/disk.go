package disk

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const (
	blobDir   = "blobs"
	tempDir   = "tmp"
	maxRefLen = 64
)

var ErrRef = errors.New("disk: invalid blob reference")

type Blobs struct {
	root string
}

func New(dataDir string) (*Blobs, error) {
	root := filepath.Join(dataDir, blobDir)
	if err := os.MkdirAll(filepath.Join(root, tempDir), 0o700); err != nil {
		return nil, err
	}
	b := &Blobs{root: root}
	if err := b.probeLink(); err != nil {
		return nil, fmt.Errorf("disk: the blob directory must support hard links: %w", err)
	}
	return b, nil
}

func (b *Blobs) probeLink() error {
	f, err := os.CreateTemp(filepath.Join(b.root, tempDir), "probe-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)
	probe := filepath.Join(b.root, tempDir, filepath.Base(tmp)+".link")
	if err := os.Link(tmp, probe); err != nil {
		return err
	}
	return os.Remove(probe)
}

func (b *Blobs) path(ref string) (string, error) {
	if ref == "" || len(ref) > maxRefLen {
		return "", ErrRef
	}
	return filepath.Join(b.root, hex.EncodeToString([]byte(ref))), nil
}

func (b *Blobs) syncDir() error {
	d, err := os.Open(b.root)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (b *Blobs) Put(ctx context.Context, ref string, r io.Reader, size int64) error {
	final, err := b.path(ref)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Join(b.root, tempDir), "blob-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	n, err := io.Copy(f, r)
	if err == nil && n != size {
		err = io.ErrUnexpectedEOF
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Link(tmp, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return store.ErrConflict
		}
		return err
	}
	os.Remove(tmp)
	return b.syncDir()
}

func (b *Blobs) Open(ctx context.Context, ref string) (io.ReadSeekCloser, int64, error) {
	p, err := b.path(ref)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, store.ErrBlobMissing
		}
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func (b *Blobs) Delete(ctx context.Context, ref string) error {
	p, err := b.path(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return b.syncDir()
}

func (b *Blobs) List(ctx context.Context) ([]store.BlobInfo, error) {
	entries, err := os.ReadDir(b.root)
	if err != nil {
		return nil, err
	}
	out := make([]store.BlobInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ref, err := hex.DecodeString(e.Name())
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, store.BlobInfo{Ref: string(ref), Modified: info.ModTime()})
	}
	return out, nil
}

func (b *Blobs) PurgeTemp(ctx context.Context, olderThan time.Time) (int, error) {
	entries, err := os.ReadDir(filepath.Join(b.root, tempDir))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.ModTime().After(olderThan) {
			continue
		}
		if err := os.Remove(filepath.Join(b.root, tempDir, e.Name())); err == nil {
			n++
		}
	}
	return n, nil
}
