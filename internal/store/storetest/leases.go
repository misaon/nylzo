package storetest

import (
	"bytes"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) leasesAndAcks() {
	t := f.t
	created := f.create(3, oneHour, false)
	leases := f.claimConcurrently(created, 3)
	if _, err := f.claim(created, baseTime, 9); err == nil {
		t.Fatal("a fourth claim was accepted")
	}
	minute := baseTime.Add(time.Minute)
	f.downloadUpToTheLimit(created, leases, minute)
	if finished, _ := f.finish(created, minute, f.blobs); finished {
		t.Fatal("released while leases are active")
	}
	f.refuseForgedLeases(created, &leases[0], minute)
	for i, lease := range leases {
		if err := f.ack(created, &lease, minute, f.blobs); err != nil {
			t.Fatal(err)
		}
		last := i == len(leases)-1
		if f.blobExists(created.ref()) == last || f.get(created).HasBlob() == last {
			t.Fatalf("ack %d: the blob must go with the last lease and not before", i+1)
		}
	}
	record := f.get(created)
	if record.State(minute) != store.StateOpened || record.Message.FirstOpenedAt == nil ||
		!record.EndedAt(minute).Equal(minute) {
		t.Fatalf("expected an opened message that ended at the last ack, got %+v", record.Message)
	}
	if err := f.download(created, &leases[0], baseTime); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("download on an acked lease: %v", err)
	}
}

func (f *fixture) downloadUpToTheLimit(created *message, leases []store.Lease, now time.Time) {
	t := f.t
	for _, lease := range leases {
		if !lease.Active(baseTime.Add(119 * time.Second)) {
			t.Fatal("a lease was not active before its expiry")
		}
		for download := 1; download <= store.MaxLeaseDownload; download++ {
			if err := f.download(created, &lease, now); err != nil {
				t.Fatalf("download %d: %v", download, err)
			}
		}
		if err := f.download(created, &lease, now); !errors.Is(err, store.ErrLeaseLimit) {
			t.Fatalf("a fourth download: %v", err)
		}
	}
	stored := f.get(created).Message.Leases
	if len(stored) != len(leases) || slices.ContainsFunc(stored, func(lease store.Lease) bool {
		return lease.Downloads != store.MaxLeaseDownload
	}) {
		t.Fatalf("the downloads were not counted per lease: %+v", stored)
	}
}

func (f *fixture) refuseForgedLeases(created *message, lease *store.Lease, now time.Time) {
	t := f.t
	forged := *lease
	forged.MAC = slices.Clone(forged.MAC)
	forged.MAC[0] ^= 1
	elsewhere := f.create(1, oneHour, false)
	if err := f.download(created, &forged, now); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("download with another lease_mac: %v", err)
	}
	if err := f.ack(created, &forged, now, f.blobs); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("ack with another lease_mac: %v", err)
	}
	if err := f.download(elsewhere, lease, now); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("a lease of another message: %v", err)
	}
}

func (f *fixture) claimConcurrently(created *message, count int) []store.Lease {
	f.t.Helper()
	results := make([]store.ClaimResult, count)
	errs := make([]error, count)
	var wait sync.WaitGroup
	for i := range count {
		wait.Go(func() {
			results[i], errs[i] = f.claim(created, baseTime.Add(time.Duration(i)*time.Millisecond), i)
		})
	}
	wait.Wait()
	leases := make([]store.Lease, 0, count)
	for i, result := range results {
		if errs[i] != nil || slices.ContainsFunc(leases, func(lease store.Lease) bool {
			return bytes.Equal(lease.MAC, result.Lease.MAC)
		}) {
			f.t.Fatalf("claim %d: %v, or its lease was handed out twice", i, errs[i])
		}
		leases = append(leases, result.Lease)
	}
	return leases
}

func (f *fixture) leaseExpiry() {
	t := f.t
	created := f.create(1, oneHour, false)
	claimed, err := f.claim(created, baseTime.Add(17*time.Second), 1)
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
	if err = f.download(created, &claimed.Lease, expiry); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("download at the expiry: %v", err)
	}
	if finished, _ := f.finish(created, expiry.Add(-time.Second), f.blobs); finished || !f.blobExists(created.ref()) {
		t.Fatal("released before the expiry of the lease")
	}
	if row := f.get(created).Row; !row.BlobPurgeAt.Equal(baseTime.Add(store.BlobPurgeStep)) ||
		!row.PurgeAt.Equal(baseTime.Add(store.TombstoneTTL+time.Hour)) {
		t.Fatalf("the last claim must move the cleanup times to the end of its lease, rounded up: %+v", row)
	}
	unattended := f.create(1, oneHour, false)
	if _, err = f.claim(unattended, baseTime.Add(17*time.Second), 2); err != nil {
		t.Fatal(err)
	}
	f.timerReleasesAtTheLeaseEnd(created, expiry)
	f.sweepReleasesAfterTheLease(unattended, expiry)
}

func (f *fixture) timerReleasesAtTheLeaseEnd(created *message, expiry time.Time) {
	t := f.t
	if finished, err := f.finish(created, expiry.Add(store.LeaseTimerSlack), f.blobs); err != nil || !finished {
		t.Fatalf("release at the end of the lease: %v %v", finished, err)
	}
	if record := f.get(created); record.HasBlob() || f.blobExists(created.ref()) ||
		!record.EndedAt(expiry).Equal(expiry) {
		t.Fatalf("the timer must delete the blob and record the minute: %+v", record.Message)
	}
}

func (f *fixture) sweepReleasesAfterTheLease(unattended *message, expiry time.Time) {
	t := f.t
	if report, err := f.sweep(f.blobs, expiry.Add(store.LeaseTimerSlack)); err != nil || report.Blobs != 0 {
		t.Fatalf("the sweep must wait for blob_purge_at: %+v %v", report, err)
	}
	report, err := f.sweep(f.blobs, baseTime.Add(store.BlobPurgeStep))
	if err != nil || report.Blobs != 1 || f.blobExists(unattended.ref()) {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if record := f.get(unattended); record.State(expiry) != store.StateOpened || !record.EndedAt(expiry).Equal(expiry) {
		t.Fatalf("a release by the sweep ends at the end of the last lease: %+v", record.Message)
	}
}

func (f *fixture) blobStaysWhileOpensRemain() {
	t := f.t
	created := f.create(2, oneHour, false)
	f.claimAndAck(created)
	if finished, err := f.finish(created, baseTime.Add(time.Minute), f.blobs); err != nil || finished ||
		!f.blobExists(created.ref()) {
		t.Fatal("the blob must stay while opens remain")
	}
	if report, _ := f.sweep(f.blobs, baseTime.Add(10*time.Minute)); report.Blobs != 0 || !f.blobExists(created.ref()) {
		t.Fatal("the sweep removed a blob with opens remaining")
	}
}

func (f *fixture) staleLeaseLeavesOtherMessages() {
	t := f.t
	first := f.create(1, oneHour, false)
	claimed, err := f.claim(first, baseTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	stale := claimed.Lease
	if err = f.ack(first, &stale, baseTime, f.blobs); err != nil {
		t.Fatal(err)
	}
	victim := f.create(1, oneHour, false)
	if _, err = f.claim(victim, baseTime, 2); err != nil {
		t.Fatal(err)
	}
	if err = f.ack(victim, &stale, baseTime, f.blobs); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("a lease of another message acked this one: %v", err)
	}
	if err = f.download(victim, &stale, baseTime); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("a lease of another message downloaded this one: %v", err)
	}
	record := f.get(victim)
	if len(record.Message.Leases) != 1 || record.Message.Leases[0].Downloads != 0 ||
		record.Message.Ending != "" || !f.blobExists(victim.ref()) {
		t.Fatalf("a stale lease of another message touched this one: %+v", record.Message)
	}
}
