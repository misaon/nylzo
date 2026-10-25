package sweep_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/sweep"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var errInjected = errors.New("test: injected storage failure")

type faultyBlobs struct {
	store.Blobs

	mutex      sync.Mutex
	failList   bool
	failDelete map[string]bool
}

func (b *faultyBlobs) Delete(ctx context.Context, ref string) error {
	b.mutex.Lock()
	failing := b.failDelete[ref]
	b.mutex.Unlock()
	if failing {
		return errInjected
	}
	return b.Blobs.Delete(ctx, ref)
}

func (b *faultyBlobs) DeleteBatch(ctx context.Context, refs []string) []error {
	errs := make([]error, len(refs))
	for i, ref := range refs {
		errs[i] = b.Delete(ctx, ref)
	}
	return errs
}

func (b *faultyBlobs) List(ctx context.Context) ([]store.BlobInfo, error) {
	b.mutex.Lock()
	failing := b.failList
	b.mutex.Unlock()
	if failing {
		return nil, errInjected
	}
	return b.Blobs.List(ctx)
}

func (b *faultyBlobs) fail(list bool, refs ...string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.failList = list
	b.failDelete = map[string]bool{}
	for _, ref := range refs {
		b.failDelete[ref] = true
	}
}

type fixture struct {
	dataDir string
	store   *sqlite.Store
	blobs   *faultyBlobs
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dataDir := t.TempDir()
	storage, err := sqlite.Open(t.Context(), filepath.Join(dataDir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	blobs, err := disk.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{dataDir: dataDir, store: storage, blobs: &faultyBlobs{Blobs: blobs}}
}

func (f fixture) putBlob(t *testing.T, ref string) {
	t.Helper()
	if err := f.blobs.Put(t.Context(), ref, strings.NewReader("x"), 1); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) createExpiring(t *testing.T, now time.Time) string {
	t.Helper()
	id, ref := core.NewID(), core.NewID().String()
	f.putBlob(t, ref)
	message := &store.Message{
		Version:        core.Version,
		CreatedAt:      now.Truncate(time.Minute),
		ExpiresAt:      store.CeilMinute(now.Add(time.Minute)),
		TTLSeconds:     60,
		OpensMax:       1,
		OpensRemaining: 1,
		Size:           1,
	}
	record := store.NewRecord(
		seal.LocatorOf(id[:]),
		message,
		core.NewSecret()[:],
		core.NewSecret()[:],
		ref,
		seal.NewKey(),
	)
	if err := f.store.Create(t.Context(), &record.Row); err != nil {
		t.Fatal(err)
	}
	return ref
}

func (f fixture) run(t *testing.T, now time.Time) (store.SweepReport, error) {
	t.Helper()
	return sweep.Run(t.Context(), f.store, f.blobs, f.dataDir, now)
}

func nextDay() time.Time {
	return time.Now().UTC().Truncate(24 * time.Hour).Add(25 * time.Hour)
}

func TestRunDeletesExpiredBlobsAndPurgesStaleUploads(t *testing.T) {
	t.Parallel()
	setup := newFixture(t)
	now := nextDay()
	id := setup.createExpiring(t, now)
	stale := func(name string) {
		if err := os.WriteFile(filepath.Join(setup.dataDir, "blobs", "tmp", name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stale("blob-first")
	report, err := setup.run(t, now.Add(store.BlobPurgeStep))
	if err != nil || report.Blobs != 1 || report.StaleTempFiles != 1 {
		t.Fatalf("first sweep: %+v %v", report, err)
	}
	if _, _, openErr := setup.blobs.Open(t.Context(), id); !errors.Is(openErr, store.ErrBlobMissing) {
		t.Fatalf("the blob of an expired message survived the sweep: %v", openErr)
	}
	stale("blob-second")
	report, err = setup.run(t, now.Add(store.BlobPurgeStep+time.Minute))
	if err != nil || report.StaleTempFiles != 1 {
		t.Fatalf("a sweep after the daily reconcile must still purge temp files: %+v %v", report, err)
	}
}

func TestReconcileRunsOncePerDayUntilItHasListedTheStore(t *testing.T) {
	t.Parallel()
	setup := newFixture(t)
	now := nextDay()
	setup.putBlob(t, "orphan-one")
	if report, err := setup.run(t, now); err != nil || report.OrphanedBlobs != 1 {
		t.Fatalf("first sweep of the day: %+v %v", report, err)
	}
	setup.putBlob(t, "orphan-two")
	if report, err := setup.run(t, now.Add(time.Hour)); err != nil || report.OrphanedBlobs != 0 {
		t.Fatalf("a second sweep reconciled twice in one day: %+v %v", report, err)
	}
	tomorrow := now.Add(24 * time.Hour)
	setup.putBlob(t, "0-stuck")
	id := setup.createExpiring(t, tomorrow)
	setup.blobs.fail(true, id)
	report, err := setup.run(t, tomorrow.Add(store.BlobPurgeStep))
	if err == nil || report.Blobs != 0 || report.OrphanedBlobs != 0 {
		t.Fatalf("a failed listing: %+v %v", report, err)
	}
	setup.blobs.fail(false, "0-stuck")
	report, err = setup.run(t, tomorrow.Add(store.BlobPurgeStep+time.Minute))
	if err == nil || report.Blobs != 1 || report.OrphanedBlobs != 1 {
		t.Fatalf(
			"a reconcile whose listing failed must run again the same day and skip a failed delete: %+v %v",
			report,
			err,
		)
	}
	report, err = setup.run(t, tomorrow.Add(store.BlobPurgeStep+2*time.Minute))
	if err != nil || report.OrphanedBlobs != 0 {
		t.Fatalf("a reconcile that listed the store must be recorded for the day: %+v %v", report, err)
	}
}

func TestLogAttributesCountEveryFailedItem(t *testing.T) {
	t.Parallel()
	setup := newFixture(t)
	now := nextDay()
	setup.blobs.fail(false, setup.createExpiring(t, now), setup.createExpiring(t, now))
	report, err := setup.run(t, now.Add(store.BlobPurgeStep))
	attributes := sweep.LogAttributes(report, err)
	got := map[any]any{}
	for i := 0; i+1 < len(attributes); i += 2 {
		got[attributes[i]] = attributes[i+1]
	}
	if got["errors"] != 2 || strings.Contains(fmt.Sprint(got["err"]), "\n") {
		t.Fatalf("want 2 failed items and only the first error: %v", got)
	}
	empty := sweep.LogAttributes(store.SweepReport{}, fmt.Errorf("%w %w", nil, nil))
	if len(empty) < 4 || empty[len(empty)-4] != "errors" || empty[len(empty)-3] != 1 {
		t.Fatalf("an empty join: %v", empty)
	}
}
