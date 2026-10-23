package server_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
)

func TestSweepOnceWithoutPepperEndsExpiredMessages(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.clock.mu.Lock()
	h.clock.now = time.Now().Add(-time.Hour)
	h.clock.mu.Unlock()
	m := newMessage(t, core.KDFNone, "", 1, 60, "sweep")
	h.mustCreate(m)
	report, err := server.SweepStore(t.Context(), h.store, h.blobs, h.dir, time.Now())
	if err != nil || report.Expired != 1 || h.blobExists(m.id) {
		t.Fatalf("sweep without pepper: %+v %v", report, err)
	}
}

func TestSweepReconcilesOncePerDay(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.startOfNextUTCDay()
	put := func(ref string) {
		if err := h.blobs.Put(t.Context(), ref, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	put("orphan-one")
	report, err := h.srv.SweepOnce(t.Context())
	if err != nil || report.OrphanedBlobs != 1 {
		t.Fatalf("first sweep of the day: %+v %v", report, err)
	}
	put("orphan-two")
	h.reopen(h.cfg)
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.OrphanedBlobs != 0 {
		t.Fatalf("a restarted server reconciled twice in one day: %+v %v", report, err)
	}
	h.clock.Advance(24 * time.Hour)
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.OrphanedBlobs != 1 {
		t.Fatalf("first sweep of the next day: %+v %v", report, err)
	}
}

func TestSweepRetriesReconcileAfterFailure(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.startOfNextUTCDay()
	fb := h.withFaultyBlobs()
	for _, ref := range []string{"0-stuck", "orphan"} {
		if err := h.blobs.Put(t.Context(), ref, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	m := newMessage(t, core.KDFNone, "", 1, 60, "expiring")
	h.mustCreate(m)
	h.clock.Advance(2 * time.Minute)
	fb.set(func(b *faultyBlobs) { b.failList = errInjected; b.failDelete[m.id.String()] = true })
	report, err := h.srv.SweepOnce(t.Context())
	if err == nil || report.Expired != 0 || report.OrphanedBlobs != 0 {
		t.Fatalf("a failed listing: %+v %v", report, err)
	}
	fb.set(func(b *faultyBlobs) { b.failList = nil; clear(b.failDelete); b.failDelete["0-stuck"] = true })
	report, err = h.srv.SweepOnce(t.Context())
	if err == nil || report.Expired != 1 || report.OrphanedBlobs != 1 {
		t.Fatalf("a reconcile whose listing failed must run again the same day and skip past a failed delete: %+v %v", report, err)
	}
	report, err = h.srv.SweepOnce(t.Context())
	if err != nil || report.OrphanedBlobs != 0 {
		t.Fatalf("a reconcile that listed the store must be recorded for the day: %+v %v", report, err)
	}
}

func TestSweepLogAttrsCountsEveryFailedItem(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	for range 2 {
		m := newMessage(t, core.KDFNone, "", 1, 60, "x")
		h.mustCreate(m)
		fb.set(func(b *faultyBlobs) { b.failDelete[m.id.String()] = true })
	}
	h.clock.Advance(2 * time.Minute)
	report, err := h.srv.SweepOnce(t.Context())
	attrs := server.SweepLogAttrs(report, err)
	got := map[any]any{}
	for i := 0; i+1 < len(attrs); i += 2 {
		got[attrs[i]] = attrs[i+1]
	}
	if got["errors"] != 2 || strings.Contains(fmt.Sprint(got["err"]), "\n") {
		t.Fatalf("want 2 failed items and only the first error: %v", got)
	}
}

func TestEverySweepPurgesStaleTempFiles(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.startOfNextUTCDay()
	stale := func(name string) {
		if err := os.WriteFile(filepath.Join(h.dir, "blobs", "tmp", name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stale("blob-first")
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.StaleTempFiles != 1 {
		t.Fatalf("first sweep: %+v %v", report, err)
	}
	stale("blob-second")
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.OrphanedBlobs != 0 || report.StaleTempFiles != 1 {
		t.Fatalf("a sweep after the daily reconcile must still purge temp files: %+v %v", report, err)
	}
}
