package storetest

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/store"
)

var ErrInjected = errors.New("storetest: injected blob failure")

type FaultyBlobs struct {
	store.Blobs

	mutex    sync.Mutex
	deletes  map[string]deleteFault
	attempts map[string]int
	open     error
	read     error
	put      error
	list     error
	onOpen   func(reference string)
}

type deleteFault struct {
	err       error
	remaining int
}

func NewFaultyBlobs(blobs store.Blobs) *FaultyBlobs {
	return &FaultyBlobs{Blobs: blobs, deletes: map[string]deleteFault{}, attempts: map[string]int{}}
}

// FailDelete fails every delete of reference with err until it is called again with a nil err.
func (b *FaultyBlobs) FailDelete(reference string, err error) {
	b.setDelete(reference, deleteFault{err: err})
}

func (b *FaultyBlobs) FailDeleteOnce(reference string, err error) {
	b.setDelete(reference, deleteFault{err: err, remaining: 1})
}

func (b *FaultyBlobs) FailOpen(err error) { b.locked(func() { b.open = err }) }

func (b *FaultyBlobs) FailRead(err error) { b.locked(func() { b.read = err }) }

func (b *FaultyBlobs) FailPut(err error) { b.locked(func() { b.put = err }) }

func (b *FaultyBlobs) FailList(err error) { b.locked(func() { b.list = err }) }

func (b *FaultyBlobs) OnOpen(hook func(reference string)) { b.locked(func() { b.onOpen = hook }) }

func (b *FaultyBlobs) DeleteAttempts(reference string) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.attempts[reference]
}

func (b *FaultyBlobs) Delete(ctx context.Context, reference string) error {
	if err := b.deleteFailure(reference); err != nil {
		return err
	}
	return b.Blobs.Delete(ctx, reference)
}

func (b *FaultyBlobs) DeleteBatch(ctx context.Context, references []string) []error {
	errs := make([]error, len(references))
	var passed []string
	var positions []int
	for i, reference := range references {
		if errs[i] = b.deleteFailure(reference); errs[i] != nil {
			continue
		}
		passed = append(passed, reference)
		positions = append(positions, i)
	}
	for i, err := range b.Blobs.DeleteBatch(ctx, passed) {
		errs[positions[i]] = err
	}
	return errs
}

func (b *FaultyBlobs) Open(ctx context.Context, reference string) (io.ReadCloser, int64, error) {
	b.mutex.Lock()
	openErr, readErr, hook := b.open, b.read, b.onOpen
	b.mutex.Unlock()
	if hook != nil {
		hook(reference)
	}
	if openErr != nil {
		return nil, 0, openErr
	}
	blob, size, err := b.Blobs.Open(ctx, reference)
	if err != nil || readErr == nil {
		return blob, size, err
	}
	return &failingReader{ReadCloser: blob, err: readErr}, size, nil
}

func (b *FaultyBlobs) PutExpiring(
	ctx context.Context,
	reference string,
	reader io.Reader,
	size int64,
	ttl time.Duration,
) (time.Time, error) {
	b.mutex.Lock()
	err := b.put
	b.mutex.Unlock()
	if err != nil {
		_, _ = io.Copy(io.Discard, reader)
		return time.Time{}, err
	}
	return b.Blobs.PutExpiring(ctx, reference, reader, size, ttl)
}

func (b *FaultyBlobs) List(ctx context.Context) ([]store.BlobInfo, error) {
	b.mutex.Lock()
	err := b.list
	b.mutex.Unlock()
	if err != nil {
		return nil, err
	}
	return b.Blobs.List(ctx)
}

func (b *FaultyBlobs) locked(change func()) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	change()
}

func (b *FaultyBlobs) setDelete(reference string, fault deleteFault) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if fault.err == nil {
		delete(b.deletes, reference)
		return
	}
	b.deletes[reference] = fault
}

func (b *FaultyBlobs) deleteFailure(reference string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.attempts[reference]++
	fault, ok := b.deletes[reference]
	if !ok {
		return nil
	}
	if fault.remaining == 1 {
		delete(b.deletes, reference)
	}
	return fault.err
}

// failingReader passes the erase block and fails inside the encrypted object, as a disk fault during a stream would.
type failingReader struct {
	io.ReadCloser

	err    error
	passed int64
}

func (f *failingReader) Read(buffer []byte) (int, error) {
	if f.passed >= erase.BlockLen {
		return 0, f.err
	}
	read, err := f.ReadCloser.Read(buffer[:min(int64(len(buffer)), erase.BlockLen-f.passed)])
	f.passed += int64(read)
	return read, err
}

type batchRecorder struct {
	store.Blobs

	store      store.Store
	rows       map[string]*message
	batches    int
	deletes    int
	references []string
	readable   []string
}

func (b *batchRecorder) Delete(ctx context.Context, reference string) error {
	b.deletes++
	return b.Blobs.Delete(ctx, reference)
}

// DeleteBatch records every blob whose key was still in its row when the batch deleted its file.
func (b *batchRecorder) DeleteBatch(ctx context.Context, references []string) []error {
	b.batches++
	b.references = append(b.references, references...)
	for _, reference := range references {
		created, known := b.rows[reference]
		if !known {
			continue
		}
		if row, err := b.store.Load(ctx, created.Row.Locator); err != nil || row.BlobWrap != nil {
			b.readable = append(b.readable, reference)
		}
	}
	return b.Blobs.DeleteBatch(ctx, references)
}
