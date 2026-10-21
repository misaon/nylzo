package disk

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const (
	blobDir = "blobs"
	tempDir = "tmp"
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
	return &Blobs{root: root}, nil
}

func (b *Blobs) path(ref string) (string, error) {
	if ref == "" || len(ref) > 64 || strings.ContainsAny(ref, "/\\") || strings.HasPrefix(ref, ".") {
		return "", ErrRef
	}
	return filepath.Join(b.root, ref), nil
}

func (b *Blobs) Put(ctx context.Context, ref string, r io.Reader, size int64) error {
	final, err := b.path(ref)
	if err != nil {
		return err
	}
	var suffix [8]byte
	rand.Read(suffix[:])
	tmp := filepath.Join(b.root, tempDir, fmt.Sprintf("%s.%x", ref, suffix))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
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
	if err == nil {
		err = os.Rename(tmp, final)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
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
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, store.BlobInfo{Ref: e.Name(), Modified: info.ModTime()})
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
