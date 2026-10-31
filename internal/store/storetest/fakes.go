package storetest

import (
	"context"
	"errors"
	"io"
	"sync"

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
	onOpen   func(ref string)
}

type deleteFault struct {
	err       error
	remaining int
}

func NewFaultyBlobs(blobs store.Blobs) *FaultyBlobs {
	return &FaultyBlobs{Blobs: blobs, deletes: map[string]deleteFault{}, attempts: map[string]int{}}
}

// FailDelete fails every delete of ref with err until it is called again with a nil err.
func (b *FaultyBlobs) FailDelete(ref string, err error) {
	b.setDelete(ref, deleteFault{err: err})
}

func (b *FaultyBlobs) FailDeleteOnce(ref string, err error) {
	b.setDelete(ref, deleteFault{err: err, remaining: 1})
}

func (b *FaultyBlobs) FailOpen(err error) { b.locked(func() { b.open = err }) }

func (b *FaultyBlobs) FailRead(err error) { b.locked(func() { b.read = err }) }

func (b *FaultyBlobs) FailPut(err error) { b.locked(func() { b.put = err }) }

func (b *FaultyBlobs) FailList(err error) { b.locked(func() { b.list = err }) }

func (b *FaultyBlobs) OnOpen(hook func(ref string)) { b.locked(func() { b.onOpen = hook }) }

func (b *FaultyBlobs) DeleteAttempts(ref string) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.attempts[ref]
}

func (b *FaultyBlobs) Delete(ctx context.Context, ref string) error {
	if err := b.deleteFailure(ref); err != nil {
		return err
	}
	return b.Blobs.Delete(ctx, ref)
}

func (b *FaultyBlobs) DeleteBatch(ctx context.Context, refs []string) []error {
	errs := make([]error, len(refs))
	var passed []string
	var positions []int
	for i, ref := range refs {
		if errs[i] = b.deleteFailure(ref); errs[i] != nil {
			continue
		}
		passed = append(passed, ref)
		positions = append(positions, i)
	}
	for i, err := range b.Blobs.DeleteBatch(ctx, passed) {
		errs[positions[i]] = err
	}
	return errs
}

func (b *FaultyBlobs) Open(ctx context.Context, ref string) (io.ReadCloser, int64, error) {
	b.mutex.Lock()
	openErr, readErr, hook := b.open, b.read, b.onOpen
	b.mutex.Unlock()
	if hook != nil {
		hook(ref)
	}
	if openErr != nil {
		return nil, 0, openErr
	}
	blob, size, err := b.Blobs.Open(ctx, ref)
	if err != nil || readErr == nil {
		return blob, size, err
	}
	return failingReader{ReadCloser: blob, err: readErr}, size, nil
}

func (b *FaultyBlobs) Put(ctx context.Context, ref string, reader io.Reader, size int64) error {
	b.mutex.Lock()
	err := b.put
	b.mutex.Unlock()
	if err != nil {
		_, _ = io.Copy(io.Discard, reader)
		return err
	}
	return b.Blobs.Put(ctx, ref, reader, size)
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

func (b *FaultyBlobs) setDelete(ref string, fault deleteFault) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if fault.err == nil {
		delete(b.deletes, ref)
		return
	}
	b.deletes[ref] = fault
}

func (b *FaultyBlobs) deleteFailure(ref string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.attempts[ref]++
	fault, ok := b.deletes[ref]
	if !ok {
		return nil
	}
	if fault.remaining == 1 {
		delete(b.deletes, ref)
	}
	return fault.err
}

type failingReader struct {
	io.ReadCloser

	err error
}

func (f failingReader) Read([]byte) (int, error) {
	return 0, f.err
}

type batchRecorder struct {
	store.Blobs

	store    store.Store
	rows     map[string]*message
	batches  int
	deletes  int
	refs     []string
	readable []string
}

func (b *batchRecorder) Delete(ctx context.Context, ref string) error {
	b.deletes++
	return b.Blobs.Delete(ctx, ref)
}

// DeleteBatch records every blob whose key was still in its row when the batch deleted its file.
func (b *batchRecorder) DeleteBatch(ctx context.Context, refs []string) []error {
	b.batches++
	b.refs = append(b.refs, refs...)
	for _, ref := range refs {
		created, known := b.rows[ref]
		if !known {
			continue
		}
		if row, err := b.store.Load(ctx, created.Row.Locator); err != nil || row.BlobWrap != nil {
			b.readable = append(b.readable, ref)
		}
	}
	return b.Blobs.DeleteBatch(ctx, refs)
}
