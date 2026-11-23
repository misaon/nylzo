package statistics

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/adminstore"
)

func TestCountsWaitWhileTheStoreFails(t *testing.T) {
	t.Parallel()
	store, err := adminstore.Open(t.Context(), filepath.Join(t.TempDir(), adminstore.FileName))
	if err != nil {
		t.Fatal(err)
	}
	counts := New(store)
	counts.Enable(true)
	noon := time.Date(2026, 11, 15, 12, 30, 0, 0, time.UTC)
	counts.Add(MetricCreated, 2, noon)
	counts.Add(MetricCreated, 3, noon.Add(time.Hour))
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = counts.Flush(t.Context(), noon.Add(time.Hour)); err == nil {
		t.Fatal("a flush into a closed store succeeded")
	}
	if err = counts.Stop(t.Context(), noon.Add(time.Hour)); err == nil {
		t.Fatal("a stop into a closed store succeeded")
	}
	if len(counts.waiting) != 2 {
		t.Fatalf("counts that could not be written were dropped: %v", counts.waiting)
	}
	if series, readErr := counts.Read(t.Context(), adminstore.Hourly, noon, noon.Add(2*time.Hour)); readErr == nil {
		t.Fatalf("a read from a closed store gave %v", series.Values)
	}
}

func TestCountsWithoutAStoreKeepNothing(t *testing.T) {
	t.Parallel()
	counts := New(nil)
	counts.Enable(true)
	noon := time.Date(2026, 11, 15, 12, 30, 0, 0, time.UTC)
	counts.Add(MetricCreated, 2, noon)
	if counts.Enabled() || len(counts.waiting) != 0 {
		t.Fatal("counts without a store counted")
	}
	if err := counts.Flush(t.Context(), noon); err != nil {
		t.Fatal(err)
	}
	if err := counts.Stop(t.Context(), noon); err != nil {
		t.Fatal(err)
	}
	if err := counts.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	series, err := counts.Read(t.Context(), adminstore.Hourly, noon, noon.Add(2*time.Hour))
	if err != nil || series.Periods != 3 || len(series.Values) != 0 {
		t.Fatalf("a read without a store gave %+v, %v", series, err)
	}
}
