package storetest

import (
	"bytes"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) leasesAndAcks() {
	created := f.create(3, oneHour, false)
	leases := f.claimConcurrently(created, 3)
	_, err := f.claim(created, baseTime, 9)
	f.fatalIf(err == nil, "a fourth claim was accepted")
	minute := baseTime.Add(time.Minute)
	f.downloadUpToTheLimit(created, leases, minute)
	finished, _ := f.finish(created, minute, f.blobs)
	f.fatalIf(finished, "released while leases are active")
	f.refuseForgedLeases(created, &leases[0], minute)
	for i, lease := range leases {
		f.must(f.ack(created, &lease, minute, f.blobs))
		last := i == len(leases)-1
		f.fatalIf(f.blobExists(created.reference()) == last || f.get(created).HasBlob() == last,
			"ack %d: the blob must go with the last lease and not before", i+1)
	}
	record := f.get(created)
	f.fatalIf(record.State(minute) != store.StateOpened || record.Message.FirstOpenedAt == nil ||
		!record.EndedAt(minute).Equal(minute),
		"expected an opened message that ended at the last ack, got %+v", record.Message)
	err = f.download(created, &leases[0], baseTime)
	f.fatalIf(!errors.Is(err, store.ErrLeaseInvalid), "download on an acked lease: %v", err)
}

// aRecordNeverGrows caps the active leases, so the length of a sealed record never tells how many were active.
func (f *fixture) aRecordNeverGrows() {
	created := f.create(20, oneDay, false)
	for seed := range store.MaxActiveLeases {
		_, err := f.claim(created, baseTime, seed)
		f.must(err)
	}
	opensBefore := f.get(created).Message.OpensRemaining
	_, err := f.claim(created, baseTime, store.MaxActiveLeases)
	busy, isBusy := errors.AsType[*store.BusyError](err)
	f.fatalIf(!isBusy || busy.RetryAfter <= 0 || f.get(created).Message.OpensRemaining != opensBefore,
		"a claim beyond %d active leases must wait and spend no open: %v", store.MaxActiveLeases, err)
	length := len(f.get(created).Row.Record)
	f.fatalIf(length != seal.RecordBlock,
		"%d active leases must fit in %d bytes, got %d", store.MaxActiveLeases, seal.RecordBlock, length)
	_, err = f.claim(created, baseTime.Add(store.LeaseDuration+time.Minute), 99)
	f.fatalIf(err != nil, "a claim after the leases ended: %v", err)
}

func (f *fixture) downloadUpToTheLimit(created *message, leases []store.Lease, now time.Time) {
	for _, lease := range leases {
		f.fatalIf(!lease.Active(baseTime.Add(119*time.Second)), "a lease was not active before its expiry")
		for download := 1; download <= store.MaxLeaseDownload; download++ {
			err := f.download(created, &lease, now)
			f.fatalIf(err != nil, "download %d: %v", download, err)
		}
		err := f.download(created, &lease, now)
		f.fatalIf(!errors.Is(err, store.ErrLeaseLimit), "a fourth download: %v", err)
	}
	stored := f.get(created).Message.Leases
	f.fatalIf(len(stored) != len(leases) || slices.ContainsFunc(stored, func(lease store.Lease) bool {
		return lease.Downloads != store.MaxLeaseDownload
	}), "the downloads were not counted per lease: %+v", stored)
}

func (f *fixture) refuseForgedLeases(created *message, lease *store.Lease, now time.Time) {
	forged := *lease
	forged.MAC = slices.Clone(forged.MAC)
	forged.MAC[0] ^= 1
	elsewhere := f.create(1, oneHour, false)
	err := f.download(created, &forged, now)
	f.fatalIf(!errors.Is(err, store.ErrLeaseInvalid), "download with another lease_mac: %v", err)
	err = f.ack(created, &forged, now, f.blobs)
	f.fatalIf(!errors.Is(err, store.ErrLeaseInvalid), "ack with another lease_mac: %v", err)
	err = f.download(elsewhere, lease, now)
	f.fatalIf(!errors.Is(err, store.ErrLeaseInvalid), "a lease of another message: %v", err)
}

func (f *fixture) claimConcurrently(created *message, count int) []store.Lease {
	f.t.Helper()
	results := make([]store.Lease, count)
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
		f.fatalIf(errs[i] != nil || slices.ContainsFunc(leases, func(lease store.Lease) bool {
			return bytes.Equal(lease.MAC, result.MAC)
		}), "claim %d: %v, or its lease was handed out twice", i, errs[i])
		leases = append(leases, result)
	}
	return leases
}

func (f *fixture) leaseExpiry() {
	created := f.create(1, oneHour, false)
	claimed, err := f.claim(created, baseTime.Add(17*time.Second), 1)
	f.must(err)
	expiry := claimed.ExpiresAt
	f.fatalIf(!expiry.Equal(baseTime.Add(3*time.Minute)),
		"a lease from 10:00:17 must end at the full minute 10:03, got %v", expiry)
	f.fatalIf(claimed.Active(expiry) || !claimed.Active(expiry.Add(-time.Millisecond)),
		"a lease must end exclusively at lease_expires_at")
	err = f.download(created, &claimed, expiry)
	f.fatalIf(!errors.Is(err, store.ErrLeaseInvalid), "download at the expiry: %v", err)
	finished, _ := f.finish(created, expiry.Add(-time.Second), f.blobs)
	f.fatalIf(finished || !f.blobExists(created.reference()), "released before the expiry of the lease")
	row := f.get(created).Row
	f.fatalIf(!row.BlobPurgeAt.Equal(baseTime.Add(store.BlobPurgeStep)) ||
		!row.PurgeAt.Equal(baseTime.Add(store.TombstoneTTL+time.Hour)),
		"the last claim must move the cleanup times to the end of its lease, rounded up: %+v", row)
	unattended := f.create(1, oneHour, false)
	_, err = f.claim(unattended, baseTime.Add(17*time.Second), 2)
	f.must(err)
	f.timerReleasesAtTheLeaseEnd(created, expiry)
	f.sweepReleasesAfterTheLease(unattended, expiry)
}

func (f *fixture) timerReleasesAtTheLeaseEnd(created *message, expiry time.Time) {
	finished, err := f.finish(created, expiry.Add(store.LeaseTimerSlack), f.blobs)
	f.fatalIf(err != nil || !finished, "release at the end of the lease: %v %v", finished, err)
	record := f.get(created)
	f.fatalIf(record.HasBlob() || f.blobExists(created.reference()) || !record.EndedAt(expiry).Equal(expiry),
		"the timer must delete the blob and record the minute: %+v", record.Message)
}

func (f *fixture) sweepReleasesAfterTheLease(unattended *message, expiry time.Time) {
	report, err := f.sweep(f.blobs, expiry.Add(store.LeaseTimerSlack))
	f.fatalIf(err != nil || report.Blobs != 0, "the sweep must wait for blob_purge_at: %+v %v", report, err)
	report, err = f.sweep(f.blobs, baseTime.Add(store.BlobPurgeStep))
	f.fatalIf(err != nil || report.Blobs != 1 || f.blobExists(unattended.reference()), "sweep: %+v %v", report, err)
	record := f.get(unattended)
	f.fatalIf(record.State(expiry) != store.StateOpened || !record.EndedAt(expiry).Equal(expiry),
		"a release by the sweep ends at the end of the last lease: %+v", record.Message)
}

func (f *fixture) blobStaysWhileOpensRemain() {
	created := f.create(2, oneHour, false)
	f.claimAndAck(created)
	finished, err := f.finish(created, baseTime.Add(time.Minute), f.blobs)
	f.fatalIf(err != nil || finished || !f.blobExists(created.reference()), "the blob must stay while opens remain")
	report, _ := f.sweep(f.blobs, baseTime.Add(10*time.Minute))
	f.fatalIf(report.Blobs != 0 || !f.blobExists(created.reference()), "the sweep removed a blob with opens remaining")
}

func (f *fixture) staleLeaseLeavesOtherMessages() {
	first := f.create(1, oneHour, false)
	claimed, err := f.claim(first, baseTime, 1)
	f.must(err)
	stale := claimed
	f.must(f.ack(first, &stale, baseTime, f.blobs))
	victim := f.create(1, oneHour, false)
	_, err = f.claim(victim, baseTime, 2)
	f.must(err)
	err = f.ack(victim, &stale, baseTime, f.blobs)
	f.fatalIf(!errors.Is(err, store.ErrLeaseInvalid), "a lease of another message acked this one: %v", err)
	err = f.download(victim, &stale, baseTime)
	f.fatalIf(!errors.Is(err, store.ErrLeaseInvalid), "a lease of another message downloaded this one: %v", err)
	record := f.get(victim)
	f.fatalIf(len(record.Message.Leases) != 1 || record.Message.Leases[0].Downloads != 0 ||
		record.Message.Ending != "" || !f.blobExists(victim.reference()),
		"a stale lease of another message touched this one: %+v", record.Message)
}
