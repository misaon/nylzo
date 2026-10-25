package storetest

import (
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) leasesAndAcks() {
	t, ctx := f.t, f.t.Context()
	message := f.create(3, oneHour, false)
	leases := f.claimConcurrently(message, 3)
	if _, err := f.claim(message, baseTime, 9); err == nil {
		t.Fatal("a fourth claim was accepted")
	}
	minute := baseTime.Add(time.Minute)
	f.downloadUpToTheLimit(message, leases, minute)
	if ref, _ := f.store.ReleaseIfDone(ctx, message.ID, minute); ref != "" {
		t.Fatal("released while leases are active")
	}
	f.refuseForgedLeases(&leases[0], minute)
	for i, lease := range leases {
		if _, err := f.store.Ack(ctx, &lease, minute); err != nil {
			t.Fatal(err)
		}
		released, err := store.Release(ctx, f.store, f.blobs, message.ID, minute)
		last := i == len(leases)-1
		if err != nil || released != last || f.blobExists(message.ID) == last {
			t.Fatalf("ack %d: released %v, %v", i+1, released, err)
		}
	}
	if tombstone := f.tombstone(message.ID); tombstone.State != store.StateOpened || tombstone.FirstOpenedAt == nil {
		t.Fatalf("expected an opened tombstone, got %+v", tombstone)
	}
	if ref, _ := f.store.ReleaseIfDone(ctx, message.ID, baseTime.Add(time.Hour)); ref != "" {
		t.Fatal("released after the tombstone")
	}
	if err := f.store.RecordDownload(ctx, &leases[0], baseTime); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("download on an acked lease: %v", err)
	}
}

func (f *fixture) downloadUpToTheLimit(message *store.Message, leases []store.Lease, now time.Time) {
	t, ctx := f.t, f.t.Context()
	for _, lease := range leases {
		if !lease.Active(baseTime.Add(119 * time.Second)) {
			t.Fatal("a lease was not active before its expiry")
		}
		for download := 1; download <= store.MaxLeaseDownload; download++ {
			if err := f.store.RecordDownload(ctx, &lease, now); err != nil {
				t.Fatalf("download %d: %v", download, err)
			}
		}
		if err := f.store.RecordDownload(ctx, &lease, now); !errors.Is(err, store.ErrLeaseLimit) {
			t.Fatalf("a fourth download: %v", err)
		}
	}
	stored, err := f.store.Leases(ctx, message.ID)
	if err != nil || len(stored) != len(leases) || slices.ContainsFunc(stored, func(lease store.Lease) bool {
		return lease.Downloads != store.MaxLeaseDownload
	}) {
		t.Fatalf("the downloads were not counted per lease: %+v %v", stored, err)
	}
}

func (f *fixture) refuseForgedLeases(lease *store.Lease, now time.Time) {
	t, ctx := f.t, f.t.Context()
	forged, elsewhere := *lease, *lease
	forged.MAC = slices.Clone(forged.MAC)
	forged.MAC[0] ^= 1
	elsewhere.ID = f.create(1, oneHour, false).ID
	for name, wrong := range map[string]store.Lease{"another lease_mac": forged, "another message id": elsewhere} {
		if err := f.store.RecordDownload(ctx, &wrong, now); !errors.Is(err, store.ErrLeaseInvalid) {
			t.Fatalf("download with %s: %v", name, err)
		}
		if _, err := f.store.Ack(ctx, &wrong, now); !errors.Is(err, store.ErrLeaseInvalid) {
			t.Fatalf("ack with %s: %v", name, err)
		}
	}
}

func (f *fixture) claimConcurrently(message *store.Message, count int) []store.Lease {
	f.t.Helper()
	results := make([]store.ClaimResult, count)
	errs := make([]error, count)
	var wait sync.WaitGroup
	for i := range count {
		wait.Go(func() {
			results[i], errs[i] = f.claim(message, baseTime.Add(time.Duration(i)*time.Millisecond), i)
		})
	}
	wait.Wait()
	leases := make([]store.Lease, 0, count)
	seen := map[int64]bool{}
	for i, result := range results {
		if errs[i] != nil || seen[result.Lease.RowID] {
			f.t.Fatalf("claim %d: %v, or its lease was handed out twice", i, errs[i])
		}
		seen[result.Lease.RowID] = true
		leases = append(leases, result.Lease)
	}
	return leases
}

func (f *fixture) leaseExpiry() {
	t, ctx := f.t, f.t.Context()
	message := f.create(1, oneHour, false)
	claimed, err := f.claim(message, baseTime.Add(17*time.Second), 1)
	if err != nil {
		t.Fatal(err)
	}
	expiry := claimed.Lease.ExpiresAt
	if !expiry.Equal(baseTime.Add(3 * time.Minute)) {
		t.Fatalf("a lease from 10:00:17 must end at the full minute 10:03, got %v", expiry)
	}
	if claimed.Lease.Active(expiry) || !claimed.Lease.Active(expiry.Add(-time.Millisecond)) {
		t.Fatal("a lease must end exclusively at lease_expires_at")
	}
	if err = f.store.RecordDownload(ctx, &claimed.Lease, expiry); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("download at the expiry: %v", err)
	}
	if ref, _ := f.store.ReleaseIfDone(ctx, message.ID, expiry.Add(-time.Second)); ref != "" {
		t.Fatal("released before the expiry")
	}
	for range 2 {
		ref, releaseErr := f.store.ReleaseIfDone(ctx, message.ID, expiry.Add(store.LeaseTimerSlack))
		if releaseErr != nil || ref != message.ID {
			t.Fatalf("ReleaseIfDone must keep returning the ref until ClearBlob: %q %v", ref, releaseErr)
		}
	}
	report, err := f.sweep(f.blobs, expiry.Add(store.LeaseTimerSlack))
	if err != nil || report.BlobsReleased != 1 || report.ExpiredLeases != 1 || f.blobExists(message.ID) {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if tombstone := f.tombstone(message.ID); tombstone.State != store.StateOpened {
		t.Fatalf("expected an opened tombstone, got %+v", tombstone)
	}
}

func (f *fixture) blobStaysWhileOpensRemain() {
	t, ctx := f.t, f.t.Context()
	message := f.create(2, oneHour, false)
	f.claimAndAck(message)
	released, err := store.Release(ctx, f.store, f.blobs, message.ID, baseTime.Add(time.Hour))
	if err != nil || released || !f.blobExists(message.ID) {
		t.Fatal("the blob must stay while opens remain")
	}
	if report, _ := f.sweep(f.blobs, baseTime.Add(10*time.Minute)); report.BlobsReleased != 0 ||
		!f.blobExists(message.ID) {
		t.Fatal("the sweep removed a blob with opens remaining")
	}
}

func (f *fixture) staleLeaseLeavesOtherMessages() {
	t, ctx := f.t, f.t.Context()
	first := f.create(1, oneHour, false)
	claimed, err := f.claim(first, baseTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	stale := claimed.Lease
	if _, err = f.store.Ack(ctx, &stale, baseTime); err != nil {
		t.Fatal(err)
	}
	victim := f.create(1, oneHour, false)
	if _, err = f.claim(victim, baseTime, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Ack(ctx, &stale, baseTime); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("a second ack of a removed lease: %v", err)
	}
	if err = f.store.RecordDownload(ctx, &stale, baseTime); err == nil {
		t.Fatal("a download on an acked lease must fail")
	}
	leases, err := f.store.Leases(ctx, victim.ID)
	if err != nil || len(leases) != 1 || leases[0].Downloads != 0 {
		t.Fatalf("a stale lease of another message touched this one: %+v %v", leases, err)
	}
	if record := f.get(victim.ID); record.Message == nil || record.Message.Ending != "" || !f.blobExists(victim.ID) {
		t.Fatalf("a stale lease of another message ended this one: %+v", record)
	}
}
