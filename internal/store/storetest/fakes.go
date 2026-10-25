package storetest

import (
	"context"
	"errors"
	"sync"

	"github.com/misaon/nylzo/internal/store"
)

var errDisk = errors.New("storetest: injected blob failure")

type failingBlobs struct {
	store.Blobs

	mutex    sync.Mutex
	failures map[string]int
	attempts map[string]int
}

func newFailingBlobs(blobs store.Blobs, alwaysFailing ...string) *failingBlobs {
	failing := &failingBlobs{Blobs: blobs, failures: map[string]int{}, attempts: map[string]int{}}
	for _, ref := range alwaysFailing {
		failing.failures[ref] = -1
	}
	return failing
}

func (b *failingBlobs) Delete(ctx context.Context, ref string) error {
	if b.fails(ref) {
		return errDisk
	}
	return b.Blobs.Delete(ctx, ref)
}

func (b *failingBlobs) DeleteBatch(ctx context.Context, refs []string) []error {
	errs := make([]error, len(refs))
	var passed []string
	var positions []int
	for i, ref := range refs {
		if b.fails(ref) {
			errs[i] = errDisk
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

func (b *failingBlobs) failOnce(ref string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.failures[ref] = 1
}

func (b *failingBlobs) attemptsOn(ref string) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.attempts[ref]
}

func (b *failingBlobs) fails(ref string) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.attempts[ref]++
	remaining, ok := b.failures[ref]
	switch {
	case !ok:
		return false
	case remaining == 1:
		delete(b.failures, ref)
	case remaining > 1:
		b.failures[ref] = remaining - 1
	default:
	}
	return true
}

// failingShred refuses to drop blob keys, which leaves a recorded release with its blob still readable.
type failingShred struct {
	store.Store
}

func (failingShred) ShredBlobs(context.Context, []store.DueBlob) error {
	return errDisk
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
