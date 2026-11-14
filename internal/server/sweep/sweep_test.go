package sweep_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/sweep"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
	"github.com/misaon/nylzo/internal/store/storetest"
)

type fixture struct {
	dataDirectory string
	store         *sqlite.Store
	blobs         *storetest.FaultyBlobs
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dataDirectory := t.TempDir()
	storage, err := sqlite.Open(t.Context(), filepath.Join(dataDirectory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	blobs, err := disk.New(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{dataDirectory: dataDirectory, store: storage, blobs: storetest.NewFaultyBlobs(blobs)}
}

func (f fixture) putBlob(t *testing.T, reference string) {
	t.Helper()
	if _, err := f.blobs.PutExpiring(
		t.Context(),
		reference,
		strings.NewReader("x"),
		1,
		time.Now().Add(time.Minute),
	); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) createExpiring(t *testing.T, now time.Time) string {
	t.Helper()
	id, reference := core.NewID(), core.NewID().String()
	f.putBlob(t, reference)
	message := &store.Message{
		Version:        envelope.Version,
		CreatedAt:      now.Truncate(time.Minute),
		ExpiresAt:      store.CeilMinute(now.Add(time.Minute)),
		TTLSeconds:     60,
		OpensMax:       1,
		OpensRemaining: 1,
		Size:           1,
	}
	record := store.NewRecord(
		seal.NewKey(),
		seal.LocatorOf(id[:]),
		message,
		core.NewSecret()[:],
		core.NewSecret()[:],
		reference,
		new(seal.NewBlobKey()),
	)
	if err := f.store.Create(t.Context(), &record.Row); err != nil {
		t.Fatal(err)
	}
	return reference
}

func (f fixture) run(t *testing.T, now time.Time) (store.SweepReport, error) {
	t.Helper()
	return sweep.Run(t.Context(), f.store, f.blobs, f.dataDirectory, now)
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
		if err := os.WriteFile(
			filepath.Join(setup.dataDirectory, "blobs", "tmp", name),
			[]byte("x"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	stale("blob-first")
	report, err := setup.run(t, now.Add(store.BlobPurgeStep))
	if err != nil || report.Blobs != 1 || report.StaleTemporaryFiles != 1 {
		t.Fatalf("first sweep: %+v %v", report, err)
	}
	if _, _, openErr := setup.blobs.Open(t.Context(), id); !errors.Is(openErr, store.ErrBlobMissing) {
		t.Fatalf("the blob of an expired message survived the sweep: %v", openErr)
	}
	stale("blob-second")
	report, err = setup.run(t, now.Add(store.BlobPurgeStep+time.Minute))
	if err != nil || report.StaleTemporaryFiles != 1 {
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
	setup.blobs.FailList(storetest.ErrInjected)
	setup.blobs.FailDelete(id, storetest.ErrInjected)
	report, err := setup.run(t, tomorrow.Add(store.BlobPurgeStep))
	if err == nil || report.Blobs != 0 || report.OrphanedBlobs != 0 {
		t.Fatalf("a failed listing: %+v %v", report, err)
	}
	setup.blobs.FailList(nil)
	setup.blobs.FailDelete(id, nil)
	setup.blobs.FailDelete("0-stuck", storetest.ErrInjected)
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

func TestFirstErrorNamesOneFailureAndNoCount(t *testing.T) {
	t.Parallel()
	setup := newFixture(t)
	now := nextDay()
	setup.blobs.FailDelete(setup.createExpiring(t, now), storetest.ErrInjected)
	setup.blobs.FailDelete(setup.createExpiring(t, now), storetest.ErrInjected)
	_, err := setup.run(t, now.Add(store.BlobPurgeStep))
	if got := sweep.FirstError(err); got != "store: delete blob: "+storetest.ErrInjected.Error() {
		t.Fatalf("want only the first of two failures: %q", got)
	}
	if sweep.FirstError(fmt.Errorf("%w %w", nil, nil)) == "" {
		t.Fatal("an empty join must still name the failure")
	}
}
