package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
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
	t.Run("AK5BackoffCap", func(t *testing.T) { testBackoffCap(t, open) })
	t.Run("AK6LeasesAck", func(t *testing.T) { testLeasesAck(t, open) })
	t.Run("AK6LeaseExpiry", func(t *testing.T) { testLeaseExpiry(t, open) })
	t.Run("AK6CrashBetweenReleaseAndDelete", func(t *testing.T) { testCrashBeforeDelete(t, open) })
	t.Run("AK6CrashBetweenDeleteAndClear", func(t *testing.T) { testCrashBeforeClear(t, open) })
	t.Run("AK6CrashBetweenDeleteAndEnd", func(t *testing.T) { testCrashBeforeEnd(t, open) })
	t.Run("EndMessageRetriesFailedDelete", func(t *testing.T) { testEndMessageDeleteFails(t, open) })
	t.Run("ReleaseKeepsRecordedEnding", func(t *testing.T) { testReleaseKeepsRecordedEnding(t, open) })
	t.Run("ExpiryOfSpentMessage", func(t *testing.T) { testExpiryOfSpentMessage(t, open) })
	t.Run("RateReserveIsAtomic", func(t *testing.T) { testRateReserve(t, open) })
	t.Run("ReconcileOrphans", func(t *testing.T) { testReconcileOrphans(t, open) })
	t.Run("ReconcileContinuesPastErrors", func(t *testing.T) { testReconcileErrors(t, open) })
	t.Run("SweepSurvivesBlobErrors", func(t *testing.T) { testSweepSurvivesBlobErrors(t, open) })
	t.Run("SweepPagesPastFailures", func(t *testing.T) { testSweepPagesPastFailures(t, open) })
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
	if _, err := store.EndMessage(f.ctx, f.s, f.b, m.ID, store.StateCancelled, baseTime); err != nil {
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
	report, err := store.SweepAll(f.ctx, f.s, f.b, atExpiry.Add(store.LeaseTimerSlack))
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
	report, err := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(time.Second))
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
	report, err := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(time.Second))
	if err != nil || report.BlobsReleased != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if rec := f.get(m.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateOpened {
		t.Fatal("sweep did not clear the blob ref and end the message")
	}
}

type failingBlobs struct {
	store.Blobs
	mu       sync.Mutex
	fail     map[string]int
	attempts map[string]int
}

var errDisk = errors.New("storetest: injected blob failure")

func newFailingBlobs(b store.Blobs, refs ...string) *failingBlobs {
	fb := &failingBlobs{Blobs: b, fail: map[string]int{}, attempts: map[string]int{}}
	for _, r := range refs {
		fb.fail[r] = -1
	}
	return fb
}

func (b *failingBlobs) failOnce(ref string) {
	b.mu.Lock()
	b.fail[ref] = 1
	b.mu.Unlock()
}

func (b *failingBlobs) Delete(ctx context.Context, ref string) error {
	b.mu.Lock()
	b.attempts[ref]++
	n, ok := b.fail[ref]
	if ok && n > 0 {
		if n == 1 {
			delete(b.fail, ref)
		} else {
			b.fail[ref] = n - 1
		}
	}
	b.mu.Unlock()
	if ok {
		return errDisk
	}
	return b.Blobs.Delete(ctx, ref)
}

func (f *fixture) createRow(ttl time.Duration) *store.Message {
	f.t.Helper()
	id := core.NewID().String()
	m := &store.Message{
		ID: id, Version: 1, State: store.StateAvailable,
		CreatedAt: baseTime, ExpiresAt: baseTime.Add(ttl), TTLSeconds: uint32(ttl / time.Second),
		OpensMax: 1, OpensRemaining: 1, PepperID: 7,
		ReadMAC: mac("read/" + id), ManageMAC: mac("manage/" + id), Size: 64, BlobRef: id,
	}
	if err := f.s.Create(f.ctx, m); err != nil {
		f.t.Fatal(err)
	}
	return m
}

func (f *fixture) crashAfterDelete(m *store.Message, state store.State) {
	f.t.Helper()
	if _, _, err := f.s.MarkEnding(f.ctx, m.ID, state, baseTime); err != nil {
		f.t.Fatal(err)
	}
	if err := f.b.Delete(f.ctx, m.BlobRef); err != nil {
		f.t.Fatal(err)
	}
}

func testCrashBeforeEnd(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(2, time.Hour, false)
	f.crashAfterDelete(m, store.StateCancelled)
	if rec := f.get(m.ID); rec.Message == nil || rec.Message.Ending != store.StateCancelled {
		t.Fatalf("the recorded ending must survive the crash: %+v", rec.Message)
	}
	var gone *store.GoneError
	if _, err := f.claim(m, baseTime, 1, false, false); !errors.As(err, &gone) || gone.State != store.StateCancelled {
		t.Fatalf("a claim on an interrupted ending: %v", err)
	}
	state, err := store.EndMessage(f.ctx, f.s, f.b, m.ID, store.StateDeleted, baseTime)
	if err != nil || state != store.StateCancelled {
		t.Fatalf("a later ending must finish the recorded one: %s %v", state, err)
	}
	if rec := f.get(m.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateCancelled {
		t.Fatalf("tombstone: %+v", rec)
	}
	swept := f.create(1, time.Hour, false)
	f.crashAfterDelete(swept, store.StateCancelled)
	burnedThenExpired := f.create(1, time.Minute, false)
	f.crashAfterDelete(burnedThenExpired, store.StateDeleted)
	report, err := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(2*time.Minute))
	if err != nil || report.Resumed != 1 || report.Expired != 1 {
		t.Fatalf("the sweep must finish interrupted endings: %+v %v", report, err)
	}
	if rec := f.get(swept.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateCancelled {
		t.Fatalf("swept tombstone: %+v", rec)
	}
	if rec := f.get(burnedThenExpired.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateDeleted {
		t.Fatalf("a recorded burn must win over a later expiry: %+v", rec)
	}
}

func testExpiryOfSpentMessage(t *testing.T, open Factory) {
	f := newFixture(t, open)
	unspent := f.create(2, time.Minute, false)
	fb := newFailingBlobs(f.b, unspent.ID)
	if _, err := store.SweepAll(f.ctx, f.s, fb, baseTime.Add(2*time.Minute)); !errors.Is(err, errDisk) {
		t.Fatalf("sweep with a failing delete: %v", err)
	}
	if rec := f.get(unspent.ID); rec.Message == nil || rec.Message.Ending != "" {
		t.Fatalf("an expired row needs no recorded ending: %+v", rec.Message)
	}
	spent := f.create(1, time.Hour, false)
	if _, err := f.claim(spent, baseTime.Add(59*time.Minute), 1, false, false); err != nil {
		t.Fatal(err)
	}
	report, err := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(time.Hour))
	if err != nil || report.Expired != 2 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if rec := f.get(unspent.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateExpired {
		t.Fatalf("unspent: %+v", rec)
	}
	if rec := f.get(spent.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateOpened {
		t.Fatalf("a spent message that expires during its lease must end as opened: %+v", rec)
	}
}

func testReleaseKeepsRecordedEnding(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Hour, false)
	res, err := f.claim(m, baseTime, 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Ack(f.ctx, res.Lease.RowID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.MarkEnding(f.ctx, m.ID, store.StateDeleted, baseTime); err != nil {
		t.Fatal(err)
	}
	if released, err := store.Release(f.ctx, f.s, f.b, m.ID, baseTime); err != nil || !released {
		t.Fatalf("release: %v %v", released, err)
	}
	if rec := f.get(m.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateDeleted {
		t.Fatalf("a release must keep the recorded burn: %+v", rec)
	}
	opened := f.create(1, time.Hour, false)
	res, err = f.claim(opened, baseTime, 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Ack(f.ctx, res.Lease.RowID); err != nil {
		t.Fatal(err)
	}
	if ref, err := f.s.ReleaseIfDone(f.ctx, opened.ID, baseTime); err != nil || ref == "" {
		t.Fatalf("release: %q %v", ref, err)
	}
	state, err := store.EndMessage(f.ctx, f.s, f.b, opened.ID, store.StateCancelled, baseTime)
	if err != nil || state != store.StateOpened {
		t.Fatalf("a cancel racing the last release must report opened: %s %v", state, err)
	}
}

func testEndMessageDeleteFails(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Hour, false)
	fb := newFailingBlobs(f.b)
	fb.failOnce(m.ID)
	if _, err := store.EndMessage(f.ctx, f.s, fb, m.ID, store.StateCancelled, baseTime); !errors.Is(err, errDisk) {
		t.Fatalf("a failed blob delete must surface: %v", err)
	}
	if rec := f.get(m.ID); rec.Message == nil || rec.Message.BlobRef != m.ID || !f.blobExists(m.ID) {
		t.Fatal("a failed blob delete must leave the message live with its blob")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	state, err := store.EndMessage(ctx, f.s, fb, m.ID, store.StateDeleted, baseTime)
	if err != nil || state != store.StateCancelled {
		t.Fatalf("EndMessage must finish the recorded ending even when the caller has gone: %s %v", state, err)
	}
	if rec := f.get(m.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateCancelled || f.blobExists(m.ID) {
		t.Fatalf("retry: %+v", rec)
	}
}

func testRateReserve(t *testing.T, open Factory) {
	f := newFixture(t, open)
	day := baseTime.Truncate(24 * time.Hour)
	hit := store.RateHit{Key: "global:d", WindowStart: day, PurgeAt: day.Add(48 * time.Hour)}
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := f.s.RateReserve(f.ctx, hit, 10)
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != 10 {
		t.Fatalf("concurrent reservations: %d granted, want exactly 10", granted)
	}
	if err := f.s.RateRelease(f.ctx, hit.Key, hit.WindowStart); err != nil {
		t.Fatal(err)
	}
	if c, ok, err := f.s.RateReserve(f.ctx, hit, 10); err != nil || !ok || c != 10 {
		t.Fatalf("a released slot must be reusable: %d %v %v", c, ok, err)
	}
	if got, _ := f.s.RateGet(f.ctx, hit.Key, hit.WindowStart); got.Count != 10 {
		t.Fatalf("count: %+v", got)
	}
}

func testReconcileOrphans(t *testing.T, open Factory) {
	f := newFixture(t, open)
	orphan := core.NewID().String()
	if err := f.b.Put(f.ctx, orphan, strings.NewReader("x"), 1); err != nil {
		t.Fatal(err)
	}
	live := f.create(1, time.Hour, false)
	orphans, _, err := store.Reconcile(f.ctx, f.s, f.b, time.Now())
	if err != nil || orphans != 0 || !f.blobExists(orphan) {
		t.Fatalf("reconcile inside the grace window must keep fresh objects: %d %v", orphans, err)
	}
	orphans, listed, err := store.Reconcile(f.ctx, f.s, f.b, time.Now().Add(store.BlobGrace+time.Minute))
	if err != nil || !listed || orphans != 1 || f.blobExists(orphan) {
		t.Fatalf("reconcile: %d %v", orphans, err)
	}
	if !f.blobExists(live.ID) {
		t.Fatal("reconcile deleted a live blob")
	}
}

func testReconcileErrors(t *testing.T, open Factory) {
	f := newFixture(t, open)
	stuckA, stuckB, gone := core.NewID().String(), core.NewID().String(), core.NewID().String()
	for _, ref := range []string{stuckA, stuckB, gone} {
		if err := f.b.Put(f.ctx, ref, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	fb := newFailingBlobs(f.b, stuckA, stuckB)
	orphans, listed, err := store.Reconcile(f.ctx, f.s, fb, time.Now().Add(store.BlobGrace+time.Minute))
	if !errors.Is(err, errDisk) || !listed || orphans != 1 || f.blobExists(gone) {
		t.Fatalf("one failing delete must not stop the others: %d %v %v", orphans, listed, err)
	}
	if fb.attempts[stuckA] != 1 || fb.attempts[stuckB] != 1 {
		t.Fatalf("reconcile stopped after the first failure: %v", fb.attempts)
	}
}

func testSweepSurvivesBlobErrors(t *testing.T, open Factory) {
	f := newFixture(t, open)
	bad := f.create(1, time.Minute, false)
	good := f.create(1, time.Minute, false)
	window := baseTime.Truncate(time.Hour)
	if _, err := f.s.RateHit(f.ctx, []store.RateHit{{Key: "k", WindowStart: window, PurgeAt: window.Add(time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	fb := newFailingBlobs(f.b, bad.ID)
	report, err := store.SweepAll(f.ctx, f.s, fb, baseTime.Add(2*time.Minute))
	if !errors.Is(err, errDisk) || report.Expired != 1 || report.Counters != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	if rec := f.get(bad.ID); rec.Message == nil || !f.blobExists(bad.ID) {
		t.Fatal("the row ended although its blob is still there")
	}
	if rec := f.get(good.ID); rec.Tombstone == nil || f.blobExists(good.ID) {
		t.Fatal("one failing blob blocked the rest of the batch")
	}
	report, err = store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(3*time.Minute))
	if err != nil || report.Expired != 1 || f.blobExists(bad.ID) {
		t.Fatalf("the next sweep must retry: %+v %v", report, err)
	}
}

func testSweepPagesPastFailures(t *testing.T, open Factory) {
	f := newFixture(t, open)
	rows := make([]string, 0, store.SweepBatch+1)
	for range store.SweepBatch + 1 {
		rows = append(rows, f.createRow(time.Minute).ID)
	}
	slices.Sort(rows)
	fb := newFailingBlobs(f.b, rows[:store.SweepBatch]...)
	ctx, cancel := context.WithTimeout(f.ctx, time.Minute)
	defer cancel()
	report, err := store.SweepAll(ctx, f.s, fb, baseTime.Add(2*time.Minute))
	if !errors.Is(err, errDisk) || report.Expired != 1 {
		t.Fatalf("a full batch of failures must not hide later rows: %+v", report)
	}
	if rec := f.get(rows[store.SweepBatch]); rec.Tombstone == nil {
		t.Fatal("the row behind a full batch of failures was not swept")
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
	report, _ := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(10*time.Minute))
	if report.BlobsReleased != 0 || !f.blobExists(m.ID) {
		t.Fatal("sweep removed a blob with opens remaining")
	}
}

func testExpiry(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, time.Minute, false)
	report, err := store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(59*time.Second))
	if err != nil || report.Expired != 0 {
		t.Fatalf("early sweep: %+v %v", report, err)
	}
	report, err = store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(61*time.Second))
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
	report, err = store.SweepAll(f.ctx, f.s, f.b, baseTime.Add(61*time.Second).Add(store.TombstoneTTL))
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
	if _, err := store.EndMessage(f.ctx, f.s, f.b, burn.ID, store.StateDeleted, baseTime); err != nil {
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
	if _, err := store.EndMessage(f.ctx, f.s, f.b, cancel.ID, store.StateCancelled, baseTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RecordDownload(f.ctx, res.Lease.RowID, baseTime.Add(2*time.Second)); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("lease survived cancel: %v", err)
	}
	if rec := f.get(cancel.ID); rec.Tombstone == nil || rec.Tombstone.State != store.StateCancelled || rec.Tombstone.FirstOpenedAt == nil {
		t.Fatal("cancel tombstone")
	}
	if _, err := store.EndMessage(f.ctx, f.s, f.b, cancel.ID, store.StateDeleted, baseTime); !errors.Is(err, store.ErrNotFound) {
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
	report, err := store.SweepAll(f.ctx, f.s, f.b, window.Add(2*time.Hour))
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
	report, err := store.SweepAll(f.ctx, f.s, f.b, window.Add(2*time.Hour))
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
	if err := f.b.Put(f.ctx, "y", bytes.NewReader([]byte("hello")), 5); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Put(f.ctx, "y", bytes.NewReader([]byte("other")), 5); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second Put of an existing ref must not replace it: %v", err)
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
	upper, lower := "AbCdEfGhIjKlMnOpQrStUw", "abcdefghijklmnopqrstuw"
	for ref, content := range map[string]string{upper: "upper!", lower: "lower!"} {
		if err := f.b.Put(f.ctx, ref, strings.NewReader(content), 6); err != nil {
			t.Fatalf("case-variant ref %s: %v", ref, err)
		}
	}
	if err := f.b.Delete(f.ctx, lower); err != nil {
		t.Fatal(err)
	}
	r, _, err = f.b.Open(f.ctx, upper)
	if err != nil {
		t.Fatalf("deleting a case-variant ref removed another object: %v", err)
	}
	data, _ = io.ReadAll(r)
	r.Close()
	if string(data) != "upper!" {
		t.Fatalf("case-variant refs share storage: %q", data)
	}
	if err := f.b.Put(f.ctx, "../escape", strings.NewReader("x"), 1); err != nil {
		t.Fatal(err)
	}
	infos, err = f.b.List(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, info := range infos {
		found = found || info.Ref == "../escape"
	}
	if !found {
		t.Fatal("a ref with path characters must be stored inside the blob store")
	}
}

func testBackoffCap(t *testing.T, open Factory) {
	f := newFixture(t, open)
	m := f.create(1, 7*24*time.Hour, true)
	now := baseTime
	var last time.Duration
	for i := 1; i <= 17; i++ {
		_, err := f.claim(m, now, 9, false, true)
		var pe *store.PassphraseError
		if !errors.As(err, &pe) {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if pe.RetryAfter > store.MaxBackoff {
			t.Fatalf("attempt %d waits %s", i, pe.RetryAfter)
		}
		last = pe.RetryAfter
		now = now.Add(last)
	}
	if last != store.MaxBackoff {
		t.Fatalf("17th failure waits %s, want %s", last, store.MaxBackoff)
	}
}
