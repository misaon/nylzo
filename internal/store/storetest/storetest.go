package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

type Factory func(t *testing.T) (store.Store, store.Blobs)

var baseTime = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

func Run(t *testing.T, open Factory) {
	t.Run("CreateGetConflict", func(t *testing.T) { testCreateGetConflict(t, open) })
	t.Run("AK3MetadataDoesNotConsume", func(t *testing.T) { testMetadataDoesNotConsume(t, open) })
	t.Run("AK4ConcurrentClaims", func(t *testing.T) { testConcurrentClaims(t, open) })
	t.Run("AK5PassphrasePolicy", func(t *testing.T) { testPassphrasePolicy(t, open) })
	t.Run("AK5DailyCap", func(t *testing.T) { testDailyCap(t, open) })
	t.Run("AK6LeasesAck", func(t *testing.T) { testLeasesAck(t, open) })
	t.Run("AK6LeaseExpiry", func(t *testing.T) { testLeaseExpiry(t, open) })
	t.Run("AK6CrashBetweenReleaseAndDelete", func(t *testing.T) { testCrashBeforeDelete(t, open) })
	t.Run("AK6CrashBetweenDeleteAndClear", func(t *testing.T) { testCrashBeforeClear(t, open) })
	t.Run("AK6CrashBetweenEndAndDelete", func(t *testing.T) { testCrashBeforeEndDelete(t, open) })
	t.Run("AK6BlobStaysWhileOpensRemain", func(t *testing.T) { testBlobStays(t, open) })
	t.Run("AK7Expiry", func(t *testing.T) { testExpiry(t, open) })
	t.Run("BurnAndCancel", func(t *testing.T) { testBurnCancel(t, open) })
	t.Run("RateHitAtomic", func(t *testing.T) { testRateHit(t, open) })
	t.Run("SweepBatches", func(t *testing.T) { testSweepBatches(t, open) })
	t.Run("BlobsIdempotentDelete", func(t *testing.T) { testBlobsDelete(t, open) })
}

type fixture struct {
	t     *testing.T
	s     store.Store
	b     store.Blobs
	ctx   context.Context
	count int
}

func newFixture(t *testing.T, open Factory) *fixture {
	t.Helper()
	s, b := open(t)
	return &fixture{t: t, s: s, b: b, ctx: context.Background()}
}

func mac(seed string) []byte {
	h := core.Hash([]byte(seed))
	return h[:]
}

func (f *fixture) create(opens uint16, ttl time.Duration, passphrase bool) *store.Message {
	f.t.Helper()
	f.count++
	id := core.NewID().String()
	m := &store.Message{
		ID: id, Version: 1, KDF: 0, State: store.StateAvailable,
		CreatedAt: baseTime, ExpiresAt: baseTime.Add(ttl), TTLSeconds: uint32(ttl / time.Second),
		OpensMax: opens, OpensRemaining: opens, PepperID: 7,
		ReadMAC: mac("read/" + id), ManageMAC: mac("manage/" + id), Size: 64, BlobRef: id,
	}
	if passphrase {
		m.KDF = 2
		m.VerifierMAC = mac("verifier/" + id)
	}
	if err := f.b.Put(f.ctx, id, bytes.NewReader(bytes.Repeat([]byte{0xAB}, 64)), 64); err != nil {
		f.t.Fatal(err)
	}
	if err := f.s.Create(f.ctx, m); err != nil {
		f.t.Fatal(err)
	}
	return m
}

func (f *fixture) blobExists(ref string) bool {
	f.t.Helper()
	r, _, err := f.b.Open(f.ctx, ref)
	if err != nil {
		if errors.Is(err, store.ErrBlobMissing) {
			return false
		}
		f.t.Fatal(err)
	}
	r.Close()
	return true
}

func (f *fixture) claim(m *store.Message, now time.Time, bucket byte, verifierOK bool, verifierSent bool) (store.ClaimResult, error) {
	f.t.Helper()
	return f.s.Claim(f.ctx, store.ClaimParams{
		ID: m.ID, Now: now, Bucket: []byte{bucket, 0, 0}, VerifierSent: verifierSent,
		VerifyMAC:     func(pepperID byte, stored []byte) bool { return verifierOK && bytes.Equal(stored, m.VerifierMAC) },
		LeaseMAC:      mac(fmt.Sprintf("lease/%s/%d/%d", m.ID, now.UnixNano(), bucket)),
		LeasePepperID: 7,
	})
}

func (f *fixture) get(id string) store.Record {
	f.t.Helper()
	rec, err := f.s.Get(f.ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return rec
}

func testCreateGetConflict(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Hour, false)
	rec := f.get(m.ID)
	if rec.Message == nil || rec.Message.OpensRemaining != 1 || rec.Message.State != store.StateAvailable || !bytes.Equal(rec.Message.ReadMAC, m.ReadMAC) || rec.Message.PepperID != 7 {
		t.Fatalf("unexpected record %+v", rec.Message)
	}
	if err := f.s.Create(f.ctx, m); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	if _, err := f.s.Get(f.ctx, core.NewID().String()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if err := store.EndMessage(f.ctx, f.s, f.b, m.ID, store.StateCancelled, baseTime); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Create(f.ctx, m); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("create over tombstone: %v", err)
	}
	rec = f.get(m.ID)
	if rec.Tombstone == nil || rec.Tombstone.State != store.StateCancelled || rec.Tombstone.PurgeAt != baseTime.Add(store.TombstoneTTL) {
		t.Fatalf("unexpected tombstone %+v", rec.Tombstone)
	}
}

func testMetadataDoesNotConsume(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Hour, false)
	for i := 0; i < 50; i++ {
		if rec := f.get(m.ID); rec.Message.OpensRemaining != 1 || rec.Message.State != store.StateAvailable {
			t.Fatal("metadata changed the message")
		}
		if ref, err := f.s.ReleaseIfDone(f.ctx, m.ID, baseTime); err != nil || ref != "" {
			t.Fatalf("release on a live message: %q %v", ref, err)
		}
	}
	if !f.blobExists(m.ID) {
		t.Fatal("blob vanished")
	}
}

func testConcurrentClaims(t *testing.T, open Factory) {
	f := newFixture(t, open)
	for i := 0; i < 100; i++ {
		m := f.create(1, time.Hour, false)
		var wg sync.WaitGroup
		results := make([]error, 2)
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				_, results[j] = f.claim(m, baseTime, byte(j), false, false)
			}(j)
		}
		wg.Wait()
		ok, gone := 0, 0
		for _, err := range results {
			var g *store.GoneError
			switch {
			case err == nil:
				ok++
			case errors.As(err, &g) && g.State == store.StateOpened:
				gone++
			default:
				t.Fatalf("iteration %d: unexpected error %v", i, err)
			}
		}
		if ok != 1 || gone != 1 {
			t.Fatalf("iteration %d: %d ok, %d gone", i, ok, gone)
		}
		if rec := f.get(m.ID); rec.Message.OpensRemaining != 0 || rec.Message.State != store.StateOpened || rec.Message.FirstOpenedAt == nil {
			t.Fatal("state after claims")
		}
	}
}

func testPassphrasePolicy(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(2, 24*time.Hour, true)
	now := baseTime
	expectFail := func(bucket byte, retry time.Duration, left int) {
		t.Helper()
		_, err := f.claim(m, now, bucket, false, true)
		var pe *store.PassphraseError
		if !errors.As(err, &pe) {
			t.Fatalf("want passphrase error, got %v", err)
		}
		if pe.RetryAfter != retry || pe.AttemptsLeft != left {
			t.Fatalf("got retry %s left %d, want retry %s left %d", pe.RetryAfter, pe.AttemptsLeft, retry, left)
		}
	}
	for i := 1; i <= 5; i++ {
		expectFail(1, 0, 5-i)
	}
	expectFail(1, 2*time.Second, 0)
	expectFail(1, 2*time.Second, 0)
	if rec := f.get(m.ID); rec.Message.FailCountTotal != 6 || rec.Message.OpensRemaining != 2 {
		t.Fatalf("counters after backoff probe: total %d opens %d", rec.Message.FailCountTotal, rec.Message.OpensRemaining)
	}
	now = now.Add(2 * time.Second)
	expectFail(1, 4*time.Second, 0)
	now = now.Add(4 * time.Second)
	expectFail(1, 8*time.Second, 0)
	now = now.Add(8 * time.Second)
	expectFail(1, 16*time.Second, 0)
	now = now.Add(16 * time.Second)
	expectFail(1, 32*time.Second, 0)
	if rec := f.get(m.ID); rec.Message.FailCountTotal != 10 || rec.Message.OpensRemaining != 2 {
		t.Fatalf("counters after tenth failure: total %d opens %d", rec.Message.FailCountTotal, rec.Message.OpensRemaining)
	}
	if _, err := f.claim(m, now, 2, true, true); err != nil {
		t.Fatalf("correct passphrase from another bucket: %v", err)
	}
	if _, err := f.claim(m, now, 1, true, true); err == nil {
		t.Fatal("backoff bucket must still wait even with the right passphrase")
	}
	if _, err := f.claim(m, now.Add(32*time.Second), 1, true, true); err != nil {
		t.Fatalf("after backoff: %v", err)
	}
	rec := f.get(m.ID)
	if rec.Message.OpensRemaining != 0 || rec.Message.FailCountTotal != 10 {
		t.Fatalf("final counters: opens %d total %d", rec.Message.OpensRemaining, rec.Message.FailCountTotal)
	}
	for i := 12; i < 40; i++ {
		now = now.Add(time.Hour)
		_, err := f.claim(m, now, 9, false, true)
		var pe *store.PassphraseError
		if errors.As(err, &pe) && pe.RetryAfter > store.MaxBackoff {
			t.Fatalf("backoff above one hour: %s", pe.RetryAfter)
		}
	}
	m2 := f.create(1, 24*time.Hour, true)
	if _, err := f.claim(m2, baseTime, 1, false, false); err == nil {
		t.Fatal("missing verifier accepted")
	}
	if rec := f.get(m2.ID); rec.Message.FailCountTotal != 1 {
		t.Fatal("missing verifier not counted")
	}
}

func testDailyCap(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, 7*24*time.Hour, true)
	now := time.Date(2026, 10, 22, 23, 0, 0, 0, time.UTC)
	for i := 0; i < store.DailyFailCap; i++ {
		_, err := f.claim(m, now, byte(i%200), false, true)
		var pe *store.PassphraseError
		if !errors.As(err, &pe) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	_, err := f.claim(m, now, 201, true, true)
	var pe *store.PassphraseError
	if !errors.As(err, &pe) || pe.RetryAfter != time.Hour || pe.AttemptsLeft != 0 {
		t.Fatalf("daily cap: %v", err)
	}
	if rec := f.get(m.ID); rec.Message.FailCountTotal != store.DailyFailCap || rec.Message.OpensRemaining != 1 {
		t.Fatal("daily cap counters")
	}
	if _, err := f.claim(m, now.Add(time.Hour), 201, true, true); err != nil {
		t.Fatalf("after midnight: %v", err)
	}
}

func testLeasesAck(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(3, time.Hour, false)
	var wg sync.WaitGroup
	results := make([]store.ClaimResult, 3)
	errs := make([]error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = f.claim(m, baseTime.Add(time.Duration(i)*time.Millisecond), byte(i), false, false)
		}(i)
	}
	wg.Wait()
	seen := map[int64]bool{}
	for i := range results {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if seen[results[i].Lease.RowID] {
			t.Fatal("duplicate lease")
		}
		seen[results[i].Lease.RowID] = true
	}
	if _, err := f.claim(m, baseTime, 9, false, false); err == nil {
		t.Fatal("fourth claim accepted")
	}
	leases, err := f.s.Leases(f.ctx, m.ID)
	if err != nil || len(leases) != 3 {
		t.Fatalf("leases: %d %v", len(leases), err)
	}
	for _, l := range leases {
		if !l.Active(baseTime.Add(119 * time.Second)) {
			t.Fatal("lease not active before expiry")
		}
		for i := 1; i <= store.MaxLeaseDownload; i++ {
			n, err := f.s.RecordDownload(f.ctx, l.RowID, baseTime.Add(time.Minute))
			if err != nil || n != i {
				t.Fatalf("download %d: %d %v", i, n, err)
			}
		}
		if _, err := f.s.RecordDownload(f.ctx, l.RowID, baseTime.Add(time.Minute)); !errors.Is(err, store.ErrLeaseLimit) {
			t.Fatalf("fourth download: %v", err)
		}
	}
	if ref, _ := f.s.ReleaseIfDone(f.ctx, m.ID, baseTime.Add(time.Minute)); ref != "" {
		t.Fatal("released while leases active")
	}
	for i, l := range leases {
		if err := f.s.Ack(f.ctx, l.RowID); err != nil {
			t.Fatal(err)
		}
		released, err := store.Release(f.ctx, f.s, f.b, m.ID, baseTime.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && (released || !f.blobExists(m.ID)) {
			t.Fatal("released before the last ack")
		}
		if i == 2 && (!released || f.blobExists(m.ID)) {
			t.Fatal("not released after the last ack")
		}
	}
	rec := f.get(m.ID)
	if rec.Tombstone == nil || rec.Tombstone.State != store.StateOpened || rec.Tombstone.FirstOpenedAt == nil {
		t.Fatalf("expected opened tombstone, got %+v", rec)
	}
	if ref, _ := f.s.ReleaseIfDone(f.ctx, m.ID, baseTime.Add(time.Hour)); ref != "" {
		t.Fatal("release after tombstone")
	}
	if _, err := f.s.RecordDownload(f.ctx, leases[0].RowID, baseTime); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("download on acked lease: %v", err)
	}
}

func testLeaseExpiry(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Hour, false)
	res, err := f.claim(m, baseTime, 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	atExpiry := baseTime.Add(store.LeaseDuration)
	if res.Lease.Active(atExpiry) || !res.Lease.Active(atExpiry.Add(-time.Millisecond)) {
		t.Fatal("lease boundary must be exclusive at lease_expires_at")
	}
	if _, err := f.s.RecordDownload(f.ctx, res.Lease.RowID, atExpiry); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("download at expiry: %v", err)
	}
	if ref, _ := f.s.ReleaseIfDone(f.ctx, m.ID, atExpiry.Add(-time.Second)); ref != "" {
		t.Fatal("released before expiry")
	}
	ref, err := f.s.ReleaseIfDone(f.ctx, m.ID, atExpiry.Add(store.LeaseTimerSlack))
	if err != nil || ref != m.ID {
		t.Fatalf("release after expiry: %q %v", ref, err)
	}
	again, _ := f.s.ReleaseIfDone(f.ctx, m.ID, atExpiry.Add(store.LeaseTimerSlack))
	if again != ref {
		t.Fatal("ReleaseIfDone must keep returning the ref until ClearBlob")
	}
	report, err := store.SweepAll(f.ctx, f.s, f.b, atExpiry.Add(store.LeaseTimerSlack), false)
	if err != nil || report.BlobsReleased != 1 || report.ExpiredLeases != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if f.blobExists(m.ID) {
		t.Fatal("blob survived the sweep")
	}
	if rec := f.get(m.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateOpened {
		t.Fatal("expected opened tombstone")
	}
}

func testCrashBeforeDelete(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Hour, false)
	res, err := f.claim(m, baseTime, 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Ack(f.ctx, res.Lease.RowID); err != nil {
		t.Fatal(err)
	}
	if ref, _ := f.s.ReleaseIfDone(f.ctx, m.ID, baseTime); ref != m.ID {
		t.Fatal("release should return the ref")
	}
	if !f.blobExists(m.ID) {
		t.Fatal("blob must still exist after the simulated crash")
	}
	report, err := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(time.Second), false)
	if err != nil || report.BlobsReleased != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if f.blobExists(m.ID) {
		t.Fatal("sweep did not delete the blob")
	}
	if rec := f.get(m.ID); rec.Tombstone == nil {
		t.Fatal("sweep did not end the message")
	}
}

func testCrashBeforeClear(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Hour, false)
	res, err := f.claim(m, baseTime, 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Ack(f.ctx, res.Lease.RowID); err != nil {
		t.Fatal(err)
	}
	ref, _ := f.s.ReleaseIfDone(f.ctx, m.ID, baseTime)
	if err := f.b.Delete(f.ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Delete(f.ctx, ref); err != nil {
		t.Fatalf("second delete must succeed: %v", err)
	}
	if rec := f.get(m.ID); rec.Message == nil || rec.Message.BlobRef != ref {
		t.Fatal("row must still carry blob_ref before ClearBlob")
	}
	report, err := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(time.Second), false)
	if err != nil || report.BlobsReleased != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if rec := f.get(m.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateOpened {
		t.Fatal("sweep did not clear the blob ref and end the message")
	}
}

func testCrashBeforeEndDelete(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Hour, false)
	ref, err := f.s.End(f.ctx, m.ID, store.StateDeleted, baseTime)
	if err != nil || ref != m.ID {
		t.Fatalf("end: %q %v", ref, err)
	}
	if !f.blobExists(m.ID) {
		t.Fatal("blob must still exist after the simulated crash")
	}
	orphans, _, err := store.Reconcile(f.ctx, f.s, f.b, time.Now())
	if err != nil || orphans != 0 {
		t.Fatalf("reconcile inside the grace window must keep fresh objects: %d %v", orphans, err)
	}
	orphans, _, err = store.Reconcile(f.ctx, f.s, f.b, time.Now().Add(store.ReconcileGrace+time.Minute))
	if err != nil || orphans != 1 {
		t.Fatalf("reconcile: %d %v", orphans, err)
	}
	if f.blobExists(m.ID) {
		t.Fatal("orphan survived reconcile")
	}
	live := f.create(1, time.Hour, false)
	orphans, _, err = store.Reconcile(f.ctx, f.s, f.b, time.Now().Add(store.ReconcileGrace+time.Minute))
	if err != nil || orphans != 0 || !f.blobExists(live.ID) {
		t.Fatalf("reconcile deleted a live blob: %d %v", orphans, err)
	}
}

func testBlobStays(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(2, time.Hour, false)
	res, err := f.claim(m, baseTime, 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Ack(f.ctx, res.Lease.RowID); err != nil {
		t.Fatal(err)
	}
	released, err := store.Release(f.ctx, f.s, f.b, m.ID, baseTime.Add(time.Hour))
	if err != nil || released || !f.blobExists(m.ID) {
		t.Fatal("blob must stay while opens remain")
	}
	report, _ := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(10*time.Minute), false)
	if report.BlobsReleased != 0 || !f.blobExists(m.ID) {
		t.Fatal("sweep removed a blob with opens remaining")
	}
}

func testExpiry(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Minute, false)
	report, err := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(59*time.Second), false)
	if err != nil || report.Expired != 0 {
		t.Fatalf("early sweep: %+v %v", report, err)
	}
	report, err = store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(61*time.Second), false)
	if err != nil || report.Expired != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if f.blobExists(m.ID) {
		t.Fatal("expired blob survived")
	}
	rec := f.get(m.ID)
	if rec.Tombstone == nil || rec.Tombstone.State != store.StateExpired {
		t.Fatal("expected expired tombstone")
	}
	if _, err := f.claim(m, baseTime.Add(2*time.Minute), 1, false, false); err == nil {
		t.Fatal("claim on expired message accepted")
	}
	var g *store.GoneError
	if _, err := f.claim(m, baseTime.Add(2*time.Minute), 1, false, false); !errors.As(err, &g) || g.State != store.StateExpired {
		t.Fatalf("claim on tombstone: %v", err)
	}
	report, err = store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(61*time.Second).Add(store.TombstoneTTL), false)
	if err != nil || report.Tombstones != 1 {
		t.Fatalf("tombstone purge: %+v %v", report, err)
	}
	if _, err := f.s.Get(f.ctx, m.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("after purge: %v", err)
	}
	late := f.create(1, time.Minute, false)
	var gone *store.GoneError
	if _, err := f.claim(late, baseTime.Add(2*time.Minute), 1, false, false); !errors.As(err, &gone) || gone.State != store.StateExpired {
		t.Fatalf("claim on expired-but-unswept row: %v", err)
	}
}

func testBurnCancel(t *testing.T, open Factory) {
	f := newFixture(t, open)
	burn := f.create(1, time.Hour, false)
	if err := store.EndMessage(f.ctx, f.s, f.b, burn.ID, store.StateDeleted, baseTime); err != nil {
		t.Fatal(err)
	}
	if f.blobExists(burn.ID) {
		t.Fatal("burn left the blob")
	}
	if rec := f.get(burn.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateDeleted || rec.Tombstone.FirstOpenedAt != nil {
		t.Fatal("burn tombstone")
	}
	cancel := f.create(2, time.Hour, false)
	res, err := f.claim(cancel, baseTime, 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EndMessage(f.ctx, f.s, f.b, cancel.ID, store.StateCancelled, baseTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RecordDownload(f.ctx, res.Lease.RowID, baseTime.Add(2*time.Second)); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("lease survived cancel: %v", err)
	}
	if rec := f.get(cancel.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateCancelled || rec.Tombstone.FirstOpenedAt == nil {
		t.Fatal("cancel tombstone")
	}
	if err := store.EndMessage(f.ctx, f.s, f.b, cancel.ID, store.StateDeleted, baseTime); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("end twice: %v", err)
	}
}

func testRateHit(t *testing.T, open Factory) {
	f := newFixture(t, open)
	window := baseTime.Truncate(time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[int64]bool{}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counts, err := f.s.RateHit(f.ctx, []store.RateHit{{Key: "create:h:abc", WindowStart: window, PurgeAt: window.Add(2 * time.Hour), Bytes: 10}, {Key: "create:d:abc", WindowStart: window, PurgeAt: window.Add(48 * time.Hour), Bytes: 10}})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			seen[counts[0].Count] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != 100 {
		t.Fatalf("counter lost increments: %d distinct values", len(seen))
	}
	counts, err := f.s.RateHit(f.ctx, []store.RateHit{{Key: "create:h:abc", WindowStart: window, PurgeAt: window.Add(2 * time.Hour), Bytes: 5}})
	if err != nil || counts[0].Count != 101 || counts[0].Bytes != 1005 {
		t.Fatalf("final count %+v %v", counts, err)
	}
	got, err := f.s.RateGet(f.ctx, "create:h:abc", window)
	if err != nil || got.Count != 101 || got.Bytes != 1005 {
		t.Fatalf("RateGet %+v %v", got, err)
	}
	if missing, err := f.s.RateGet(f.ctx, "nope", window); err != nil || missing.Count != 0 {
		t.Fatalf("RateGet missing %+v %v", missing, err)
	}
	other, _ := f.s.RateHit(f.ctx, []store.RateHit{{Key: "create:h:abc", WindowStart: window.Add(time.Hour), PurgeAt: window.Add(3 * time.Hour)}})
	if other[0].Count != 1 {
		t.Fatal("new window must start at 1")
	}
	report, err := store.SweepAll(f.ctx, f.s, f.b, window.Add(2*time.Hour), false)
	if err != nil || report.Counters != 1 {
		t.Fatalf("counter purge: %+v %v", report, err)
	}
}

func testSweepBatches(t *testing.T, open Factory) {
	f := newFixture(t, open)
	window := baseTime
	hits := make([]store.RateHit, 0, 2500)
	for i := 0; i < 2500; i++ {
		hits = append(hits, store.RateHit{Key: fmt.Sprintf("k%d", i), WindowStart: window, PurgeAt: window.Add(time.Hour)})
	}
	if _, err := f.s.RateHit(f.ctx, hits); err != nil {
		t.Fatal(err)
	}
	report, err := store.SweepAll(f.ctx, f.s, f.b, window.Add(2*time.Hour), false)
	if err != nil || report.Counters != 2500 {
		t.Fatalf("batched purge: %+v %v", report, err)
	}
}

func testBlobsDelete(t *testing.T, open Factory) {
	f := newFixture(t, open)
	if err := f.b.Delete(f.ctx, core.NewID().String()); err != nil {
		t.Fatalf("delete of a missing object must succeed: %v", err)
	}
	if err := f.b.Put(f.ctx, "x", bytes.NewReader([]byte("short")), 10); err == nil {
		t.Fatal("short body accepted")
	}
	if f.blobExists("x") {
		t.Fatal("partial object left behind")
	}
	if err := f.b.Put(f.ctx, "../escape", bytes.NewReader([]byte("x")), 1); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := f.b.Put(f.ctx, "y", bytes.NewReader([]byte("hello")), 5); err != nil {
		t.Fatal(err)
	}
	r, size, err := f.b.Open(f.ctx, "y")
	if err != nil || size != 5 {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	if string(data) != "hello" {
		t.Fatal("content")
	}
	infos, err := f.b.List(f.ctx)
	if err != nil || len(infos) != 1 || infos[0].Ref != "y" {
		t.Fatalf("list: %+v %v", infos, err)
	}
}
