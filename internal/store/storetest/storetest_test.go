package storetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

type recordingTB struct {
	testing.TB

	fatal  []string
	failed []string
}

func (*recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, arguments ...any) {
	r.fatal = append(r.fatal, fmt.Sprintf(format, arguments...))
}

func (r *recordingTB) Errorf(format string, arguments ...any) {
	r.failed = append(r.failed, fmt.Sprintf(format, arguments...))
}

func TestAFailedCheckStopsOrMarksTheContract(t *testing.T) {
	t.Parallel()
	recorder := &recordingTB{TB: t}
	checks := &fixture{t: recorder}
	checks.must(nil)
	checks.fatalIf(false, "passed")
	checks.errorIf(false, "passed")
	checks.must(ErrInjected)
	checks.fatalIf(true, "stopped at %d", 1)
	checks.errorIf(true, "marked %s", "failed")
	if !slices.Equal(recorder.fatal, []string{ErrInjected.Error(), "stopped at 1"}) ||
		!slices.Equal(recorder.failed, []string{"marked failed"}) {
		t.Fatalf("fatal %q, failed %q", recorder.fatal, recorder.failed)
	}
}

type memoryBlobs struct {
	store.Blobs

	deleted []string
}

func (b *memoryBlobs) Delete(_ context.Context, reference string) error {
	b.deleted = append(b.deleted, reference)
	return nil
}

func (b *memoryBlobs) DeleteBatch(_ context.Context, references []string) []error {
	b.deleted = append(b.deleted, references...)
	return make([]error, len(references))
}

type rowsByLocator struct {
	store.Store

	rows map[seal.Locator]store.Row
}

func (s *rowsByLocator) Load(_ context.Context, locator seal.Locator) (store.Row, error) {
	row, ok := s.rows[locator]
	if !ok {
		return store.Row{}, store.ErrNotFound
	}
	return row, nil
}

func TestTheBatchRecorderCatchesASweepThatDeletesTooEarly(t *testing.T) {
	t.Parallel()
	readable := &message{}
	readable.Row = store.Row{Locator: seal.Locator{1}, BlobReference: "readable", BlobWrap: []byte{1}}
	forgotten := &message{}
	forgotten.Row = store.Row{Locator: seal.Locator{2}, BlobReference: "forgotten"}
	missing := &message{}
	missing.Row = store.Row{Locator: seal.Locator{3}, BlobReference: "missing"}
	blobs := &memoryBlobs{}
	recorder := &batchRecorder{
		Blobs: blobs,
		store: &rowsByLocator{rows: map[seal.Locator]store.Row{
			readable.Row.Locator:  readable.Row,
			forgotten.Row.Locator: forgotten.Row,
		}},
		rows: map[string]*message{"readable": readable, "forgotten": forgotten, "missing": missing},
	}
	if err := recorder.Delete(t.Context(), "alone"); err != nil {
		t.Fatal(err)
	}
	recorder.DeleteBatch(t.Context(), []string{"unknown", "readable", "forgotten", "missing"})
	if recorder.deletes != 1 || recorder.batches != 1 ||
		!slices.Equal(recorder.readable, []string{"readable", "missing"}) ||
		len(blobs.deleted) != 5 {
		t.Fatalf("recorded %+v, deleted %v", recorder, blobs.deleted)
	}
}

func TestAFailureSetForADeleteCanBeLifted(t *testing.T) {
	t.Parallel()
	blobs := &memoryBlobs{}
	faulty := NewFaultyBlobs(blobs)
	faulty.FailDelete("blob", ErrInjected)
	if err := faulty.Delete(t.Context(), "blob"); !errors.Is(err, ErrInjected) {
		t.Fatalf("a delete set to fail: %v", err)
	}
	faulty.FailDelete("blob", nil)
	if err := faulty.Delete(t.Context(), "blob"); err != nil || faulty.DeleteAttempts("blob") != 2 {
		t.Fatalf("a delete whose failure was lifted: %v", err)
	}
}
