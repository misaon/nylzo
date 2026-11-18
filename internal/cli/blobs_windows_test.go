package cli

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

// The disk store needs a directory sync, which Windows does not offer, so the test server keeps blobs in memory there.
func testBlobs(string) (store.Blobs, error) { //nolint:ireturn // the blob store differs by OS
	return &memoryBlobs{contents: map[string][]byte{}, purgeAt: map[string]time.Time{}}, nil
}

type memoryBlobs struct {
	mutex    sync.Mutex
	contents map[string][]byte
	purgeAt  map[string]time.Time
}

type blobReader struct{ *bytes.Reader }

func (blobReader) Close() error { return nil }

func (b *memoryBlobs) PutExpiring(
	_ context.Context,
	reference string,
	content io.Reader,
	size int64,
	expiresAt time.Time,
) (time.Time, error) {
	data, err := io.ReadAll(io.LimitReader(content, size))
	if err != nil {
		return time.Time{}, err
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.contents[reference], b.purgeAt[reference] = data, expiresAt
	return time.Now(), nil
}

func (b *memoryBlobs) Open(_ context.Context, reference string) (io.ReadSeekCloser, int64, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	data, ok := b.contents[reference]
	if !ok {
		return nil, 0, store.ErrNotFound
	}
	return blobReader{bytes.NewReader(data)}, int64(len(data)), nil
}

func (b *memoryBlobs) Delete(_ context.Context, reference string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	delete(b.contents, reference)
	delete(b.purgeAt, reference)
	return nil
}

func (b *memoryBlobs) DeleteBatch(ctx context.Context, references []string) []error {
	errs := make([]error, len(references))
	for i, reference := range references {
		errs[i] = b.Delete(ctx, reference)
	}
	return errs
}

func (b *memoryBlobs) List(context.Context) ([]store.BlobInfo, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	listed := make([]store.BlobInfo, 0, len(b.contents))
	for reference, purgeAt := range b.purgeAt {
		listed = append(listed, store.BlobInfo{Reference: reference, PurgeAt: purgeAt})
	}
	return listed, nil
}

func (*memoryBlobs) PurgeTemporary(context.Context, time.Time) (int, error) { return 0, nil }

func (*memoryBlobs) Replenish(context.Context) error { return nil }

func (*memoryBlobs) Restamp(context.Context, time.Time) error { return nil }
