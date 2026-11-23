package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

var (
	errStorage  = errors.New("storage failure")
	testMoment  = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	testLocator = seal.LocatorOf([]byte("message"))
)

type failingMessages struct {
	Messages

	update func(change func(row *Row) error) error
	shred  error
	forget error
	known  error
}

func (m *failingMessages) Update(_ context.Context, _ seal.Locator, change func(row *Row) error) error {
	return m.update(change)
}

func (m *failingMessages) ShredBlobs(context.Context, []DueBlob) error { return m.shred }

func (m *failingMessages) ForgetBlobs(context.Context, []DueBlob) error { return m.forget }

func (m *failingMessages) KnownBlobReferences(context.Context) (map[string]struct{}, error) {
	return map[string]struct{}{}, m.known
}

type listedBlobs struct {
	Blobs

	listed  []BlobInfo
	deleted []string
}

func (b *listedBlobs) List(context.Context) ([]BlobInfo, error) { return b.listed, nil }

func (b *listedBlobs) Delete(_ context.Context, reference string) error {
	b.deleted = append(b.deleted, reference)
	return nil
}

func (b *listedBlobs) DeleteBatch(_ context.Context, references []string) []error {
	b.deleted = append(b.deleted, references...)
	return make([]error, len(references))
}

func newTestRecord() Record {
	message := &Message{
		Version:        1,
		CreatedAt:      testMoment,
		ExpiresAt:      testMoment.Add(time.Hour),
		OpensMax:       1,
		OpensRemaining: 1,
	}
	blob := seal.NewBlobKey()
	return NewRecord(seal.NewKey(), testLocator, message, []byte("read"), []byte("manage"), "blob", &blob)
}

func duePage(references ...string) []DueBlob {
	page := make([]DueBlob, 0, len(references))
	for _, reference := range references {
		page = append(page, DueBlob{Locator: testLocator, Reference: reference})
	}
	return page
}

func passRow(row *Row) func(change func(row *Row) error) error {
	return func(change func(row *Row) error) error {
		passed := *row
		return change(&passed)
	}
}

func TestARowThatNoLongerOpensUnderTheRecordKeyFailsTheUpdate(t *testing.T) {
	t.Parallel()
	record := newTestRecord()
	other := newTestRecord()
	messages := &failingMessages{update: passRow(&other.Row)}
	if err := TakeBack(t.Context(), messages, &record, Attempt{}, testMoment); !errors.Is(err, seal.ErrOpen) {
		t.Fatalf("a row sealed under another record key: %v", err)
	}
}

func TestFinishStopsWhenTheReleaseFailsOrTheBlobIsGone(t *testing.T) {
	t.Parallel()
	spent := newTestRecord()
	spent.Message.OpensRemaining = 0
	spent.reseal()
	failing := &failingMessages{update: func(func(row *Row) error) error { return errStorage }}
	if finished, err := Finish(t.Context(), failing, nil, &spent, testMoment); finished || !errors.Is(err, errStorage) {
		t.Fatalf("a failed release: %v %v", finished, err)
	}
	forgotten := spent.Row
	forgotten.BlobReference, forgotten.BlobWrap = "", nil
	swept := &failingMessages{update: passRow(&forgotten)}
	if finished, err := Finish(t.Context(), swept, nil, &spent, testMoment); finished || err != nil {
		t.Fatalf("a blob the sweep forgot meanwhile: %v %v", finished, err)
	}
}

func TestAFailedShredKeepsTheBlobFile(t *testing.T) {
	t.Parallel()
	messages := &failingMessages{shred: errStorage}
	blobs := &listedBlobs{}
	if err := DeleteBlob(t.Context(), messages, blobs, testLocator, "blob"); !errors.Is(err, errStorage) {
		t.Fatalf("a failed shred: %v", err)
	}
	page := duePage("blob")
	if deleted, err := deleteBlobPage(t.Context(), messages, blobs, page); deleted != 0 || !errors.Is(err, errStorage) {
		t.Fatalf("a failed shred of a page: %d %v", deleted, err)
	}
	if len(blobs.deleted) != 0 {
		t.Fatalf("a blob whose key may still be readable was deleted: %v", blobs.deleted)
	}
}

func TestAFailedForgetCountsNoDeletedBlob(t *testing.T) {
	t.Parallel()
	messages := &failingMessages{forget: errStorage}
	page := duePage("blob")
	if deleted, err := deleteBlobPage(t.Context(), messages, &listedBlobs{}, page); deleted != 0 ||
		!errors.Is(err, errStorage) {
		t.Fatalf("a failed forget: %d %v", deleted, err)
	}
}

func TestReconcileStopsWithoutTheKnownBlobsOrItsCaller(t *testing.T) {
	t.Parallel()
	orphan := &listedBlobs{listed: []BlobInfo{{Reference: "orphan", PurgeAt: testMoment}}}
	if report, err := Reconcile(t.Context(), &failingMessages{known: errStorage}, orphan, testMoment); report.Listed ||
		!errors.Is(err, errStorage) {
		t.Fatalf("unknown blob references: %+v %v", report, err)
	}
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := Reconcile(gone, &failingMessages{}, orphan, testMoment)
	if err != nil || !report.Listed || report.Orphans != 0 || len(orphan.deleted) != 0 {
		t.Fatalf("a reconcile whose caller left deleted on: %+v %v %v", report, err, orphan.deleted)
	}
}

func TestPassphraseStatusAfterTheLifetimeCapWaitsForTheExpiry(t *testing.T) {
	t.Parallel()
	message := &Message{FailCountTotal: MaxFailedAttempts, ExpiresAt: testMoment.Add(time.Hour)}
	if status := PassphraseStatus(message, testMoment); status.RetryAfter != time.Hour || status.AttemptsLeft != 0 {
		t.Fatalf("after the lifetime cap: %+v", status)
	}
	if status := PassphraseStatus(message, testMoment.Add(2*time.Hour)); status.RetryAfter != 0 {
		t.Fatalf("after the expiry: %+v", status)
	}
}

func TestRefuseAttemptAnswersAnEndedOrLockedMessage(t *testing.T) {
	t.Parallel()
	record := newTestRecord()
	if err := RefuseAttempt(&record, testMoment); err != nil {
		t.Fatalf("a live message: %v", err)
	}
	record.Message.NextAttemptAt = testMoment.Add(time.Minute)
	if refusal, ok := errors.AsType[*PassphraseError](RefuseAttempt(&record, testMoment)); !ok ||
		refusal.RetryAfter != time.Minute {
		t.Fatalf("a message in its wait: %v", refusal)
	}
	if gone, ok := errors.AsType[*GoneError](RefuseAttempt(&record, testMoment.Add(time.Hour))); !ok ||
		gone.State != StateExpired {
		t.Fatalf("an expired message: %v", gone)
	}
}

func TestRefusalsNameWhatTheCallerNeeds(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		err  error
		want []string
	}{
		{&GoneError{State: StateCancelled}, []string{string(StateCancelled)}},
		{&BusyError{RetryAfter: time.Minute}, []string{"1m0s"}},
		{&PassphraseError{RetryAfter: time.Hour, AttemptsLeft: 3}, []string{"1h0m0s", "3"}},
	} {
		for _, want := range testCase.want {
			if !strings.Contains(testCase.err.Error(), want) {
				t.Errorf("%T must name %s: %v", testCase.err, want, testCase.err)
			}
		}
	}
}
