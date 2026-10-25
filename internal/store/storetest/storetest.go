package storetest

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

type Factory func(t *testing.T) (store.Store, store.Blobs)

type contract struct {
	name  string
	check func(f *fixture)
}

var contracts = []contract{
	{"CreateGetConflict", (*fixture).createGetConflict},
	{"AK3MetadataDoesNotConsume", (*fixture).metadataDoesNotConsume},
	{"AK4ConcurrentClaims", (*fixture).concurrentClaims},
	{"AK5PassphraseBackoff", (*fixture).passphraseBackoff},
	{"AK5LifetimeCap", (*fixture).lifetimeCap},
	{"AK6LeasesAndAcks", (*fixture).leasesAndAcks},
	{"AK6LeaseExpiry", (*fixture).leaseExpiry},
	{"AK6BlobStaysWhileOpensRemain", (*fixture).blobStaysWhileOpensRemain},
	{"AK6CrashAroundTheBlobDelete", (*fixture).crashAroundTheBlobDelete},
	{"AK6CrashBeforeTheEnd", (*fixture).crashBeforeTheEnd},
	{"StaleLeaseLeavesOtherMessages", (*fixture).staleLeaseLeavesOtherMessages},
	{"FailedDeleteKeepsTheMessage", (*fixture).failedDeleteKeepsTheMessage},
	{"RecordedEndingWins", (*fixture).recordedEndingWins},
	{"ReleaseAfterExpiryRecordsNothing", (*fixture).releaseAfterExpiryRecordsNothing},
	{"BurnAndCancel", (*fixture).burnAndCancel},
	{"AK7Expiry", (*fixture).expiry},
	{"SweepSurvivesBlobErrors", (*fixture).sweepSurvivesBlobErrors},
	{"SweepPagesPastFailures", (*fixture).sweepPagesPastFailures},
	{"SweepEndsAnExpiredPageTogether", (*fixture).sweepEndsAnExpiredPageTogether},
	{"SweepPurgesInBatches", (*fixture).sweepPurgesInBatches},
	{"Reconcile", (*fixture).reconcile},
	{"RateReserveIsAtomic", (*fixture).rateReserveIsAtomic},
	{"RateReserveBatch", (*fixture).rateReserveBatch},
	{"RateHitIsAtomic", (*fixture).rateHitIsAtomic},
	{"CreateCountsWithTheRow", (*fixture).createCountsWithTheRow},
	{"BlobsPutOpenDelete", (*fixture).blobsPutOpenDelete},
	{"BlobsDeleteBatch", (*fixture).blobsDeleteBatch},
}

var baseTime = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

const (
	oneMinute uint32 = 60
	oneHour   uint32 = 60 * oneMinute
	oneDay    uint32 = 24 * oneHour
	oneWeek   uint32 = 7 * oneDay
)

func Run(t *testing.T, open Factory) {
	t.Helper()
	for _, test := range contracts {
		t.Run(test.name, func(t *testing.T) { test.check(newFixture(t, open)) })
	}
}

type fixture struct {
	t     *testing.T
	store store.Store
	blobs store.Blobs
}

func newFixture(t *testing.T, open Factory) *fixture {
	t.Helper()
	messages, blobs := open(t)
	return &fixture{t: t, store: messages, blobs: blobs}
}

func (f *fixture) create(opens uint16, ttlSeconds uint32, passphrase bool) *store.Message {
	f.t.Helper()
	message := newMessage(opens, ttlSeconds, passphrase)
	if err := f.blobs.Put(f.t.Context(), message.ID, bytes.NewReader(bytes.Repeat([]byte{0xAB}, 64)), 64); err != nil {
		f.t.Fatal(err)
	}
	if err := f.store.Create(f.t.Context(), message); err != nil {
		f.t.Fatal(err)
	}
	return message
}

func (f *fixture) createRow(ttlSeconds uint32) *store.Message {
	f.t.Helper()
	message := newMessage(1, ttlSeconds, false)
	if err := f.store.Create(f.t.Context(), message); err != nil {
		f.t.Fatal(err)
	}
	return message
}

func (f *fixture) claim(message *store.Message, now time.Time, seed int) (store.ClaimResult, error) {
	return f.store.Claim(f.t.Context(), claimParams(message, now, seed))
}

func (f *fixture) claimWithVerifier(
	message *store.Message,
	now time.Time,
	seed int,
	correct bool,
) (store.ClaimResult, error) {
	params := claimParams(message, now, seed)
	params.VerifierSent = true
	params.VerifyMAC = func(_ byte, stored []byte) bool { return correct && bytes.Equal(stored, message.VerifierMAC) }
	return f.store.Claim(f.t.Context(), params)
}

func (f *fixture) claimAndAck(message *store.Message) string {
	f.t.Helper()
	claimed, err := f.claim(message, baseTime, 1)
	if err != nil {
		f.t.Fatal(err)
	}
	ref, err := f.store.Ack(f.t.Context(), claimed.Lease, baseTime)
	if err != nil {
		f.t.Fatal(err)
	}
	return ref
}

func (f *fixture) get(id string) store.Record {
	f.t.Helper()
	record, err := f.store.Get(f.t.Context(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return record
}

func (f *fixture) tombstone(id string) *store.Tombstone {
	f.t.Helper()
	record := f.get(id)
	if record.Tombstone == nil {
		f.t.Fatalf("no tombstone yet: %+v", record.Message)
	}
	return record.Tombstone
}

func (f *fixture) blobExists(ref string) bool {
	f.t.Helper()
	content, _, err := f.blobs.Open(f.t.Context(), ref)
	if errors.Is(err, store.ErrBlobMissing) {
		return false
	}
	if err != nil {
		f.t.Fatal(err)
	}
	content.Close()
	return true
}

func (f *fixture) end(id string, state store.State, now time.Time) (store.State, error) {
	return store.EndMessage(f.t.Context(), f.store, f.blobs, id, state, now)
}

func (f *fixture) sweep(blobs store.Blobs, now time.Time) (store.SweepReport, error) {
	return store.SweepAll(f.t.Context(), f.store, blobs, now)
}

func (f *fixture) crashAfterDelete(message *store.Message, state store.State) {
	f.t.Helper()
	if _, _, _, err := f.store.MarkEnding(f.t.Context(), message.ID, state, baseTime); err != nil {
		f.t.Fatal(err)
	}
	if err := f.blobs.Delete(f.t.Context(), message.BlobRef); err != nil {
		f.t.Fatal(err)
	}
}

func newMessage(opens uint16, ttlSeconds uint32, passphrase bool) *store.Message {
	id := core.NewID().String()
	message := &store.Message{
		ID:             id,
		Version:        1,
		State:          store.StateAvailable,
		CreatedAt:      baseTime,
		ExpiresAt:      baseTime.Add(time.Duration(ttlSeconds) * time.Second),
		TTLSeconds:     ttlSeconds,
		OpensMax:       opens,
		OpensRemaining: opens,
		PepperID:       7,
		ReadMAC:        mac("read/" + id),
		ManageMAC:      mac("manage/" + id),
		Size:           64,
		BlobRef:        id,
	}
	if passphrase {
		message.KDF = 1
		message.VerifierMAC = mac("verifier/" + id)
	}
	return message
}

func claimParams(message *store.Message, now time.Time, seed int) store.ClaimParams {
	return store.ClaimParams{
		ID:            message.ID,
		Clock:         func() time.Time { return now },
		LeaseMAC:      mac(fmt.Sprintf("lease/%s/%d/%d", message.ID, now.UnixNano(), seed)),
		LeasePepperID: 7,
	}
}

func mac(seed string) []byte {
	sum := sha256.Sum256([]byte(seed))
	return sum[:]
}

func goneState(err error) store.State {
	if gone, ok := errors.AsType[*store.GoneError](err); ok {
		return gone.State
	}
	return ""
}
