package storetest

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

type Factory func(t *testing.T) (store.Store, store.Blobs)

type contract struct {
	name  string
	check func(f *fixture)
}

var contracts = []contract{
	{"CreateLoadConflict", (*fixture).createLoadConflict},
	{"OnlyThePresentedKeyOpensARecord", (*fixture).onlyThePresentedKeyOpens},
	{"AK3MetadataDoesNotConsume", (*fixture).metadataDoesNotConsume},
	{"AK4ConcurrentClaims", (*fixture).concurrentClaims},
	{"AK5PassphraseBackoff", (*fixture).passphraseBackoff},
	{"AK5LifetimeCap", (*fixture).lifetimeCap},
	{"AK6LeasesAndAcks", (*fixture).leasesAndAcks},
	{"AK6LeaseExpiry", (*fixture).leaseExpiry},
	{"ARecordNeverGrows", (*fixture).aRecordNeverGrows},
	{"AK6BlobStaysWhileOpensRemain", (*fixture).blobStaysWhileOpensRemain},
	{"AK6CrashAroundTheBlobDelete", (*fixture).crashAroundTheBlobDelete},
	{"AK6CrashBeforeTheEnd", (*fixture).crashBeforeTheEnd},
	{"StaleLeaseLeavesOtherMessages", (*fixture).staleLeaseLeavesOtherMessages},
	{"FailedDeleteLeavesNothingToRead", (*fixture).failedDeleteLeavesNothingToRead},
	{"RecordedEndingWins", (*fixture).recordedEndingWins},
	{"ReleaseAfterExpiryRecordsNothing", (*fixture).releaseAfterExpiryRecordsNothing},
	{"BurnAndCancel", (*fixture).burnAndCancel},
	{"TakeDownWithoutTheKey", (*fixture).takeDownWithoutTheKey},
	{"ReportsPerMessageAndDay", (*fixture).reportsPerMessageAndDay},
	{"AK7Expiry", (*fixture).expiry},
	{"SweepSurvivesBlobErrors", (*fixture).sweepSurvivesBlobErrors},
	{"SweepPagesPastFailures", (*fixture).sweepPagesPastFailures},
	{"SweepDeletesAPageTogether", (*fixture).sweepDeletesAPageTogether},
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
	blobSize         = 64
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

// message is what a test keeps of a created message: the sealed record and the public keys that open it.
type message struct {
	store.Record

	readKey   []byte
	manageKey []byte
}

func (m *message) ref() string {
	return m.Row.BlobRef
}

func (f *fixture) create(opens uint16, ttlSeconds uint32, passphrase bool) *message {
	f.t.Helper()
	created := f.createRow(opens, ttlSeconds, passphrase)
	if err := f.blobs.Put(
		f.t.Context(),
		created.ref(),
		bytes.NewReader(bytes.Repeat([]byte{0xAB}, blobSize)),
		blobSize,
	); err != nil {
		f.t.Fatal(err)
	}
	return created
}

func (f *fixture) createRow(opens uint16, ttlSeconds uint32, passphrase bool) *message {
	f.t.Helper()
	id := core.NewID()
	readKey, manageKey := core.NewSecret()[:], core.NewSecret()[:]
	blobRef := core.NewID()
	record := store.NewRecord(
		seal.LocatorOf(id[:]),
		newMessage(opens, ttlSeconds, passphrase),
		readKey,
		manageKey,
		core.Encode(blobRef[:]),
		seal.NewKey(),
	)
	if err := f.store.Create(f.t.Context(), &record.Row); err != nil {
		f.t.Fatal(err)
	}
	return &message{Record: record, readKey: readKey, manageKey: manageKey}
}

// get opens the stored row again with the record key, as the next request would.
func (f *fixture) get(created *message) *store.Record {
	f.t.Helper()
	row, err := f.store.Load(f.t.Context(), created.Row.Locator)
	if err != nil {
		f.t.Fatal(err)
	}
	record, err := store.OpenRecord(&row, created.Key)
	if err != nil {
		f.t.Fatal(err)
	}
	return &record
}

func (f *fixture) claim(created *message, now time.Time, seed int) (store.ClaimResult, error) {
	record := f.get(created)
	return store.Claim(f.t.Context(), f.store, record, claimParams(record, now, seed))
}

func (f *fixture) claimWithVerifier(
	created *message,
	now time.Time,
	seed int,
	correct bool,
) (store.ClaimResult, error) {
	record := f.get(created)
	parameters := claimParams(record, now, seed)
	parameters.VerifierSent = true
	parameters.VerifyMAC = func(_ byte, stored []byte) bool {
		return correct && bytes.Equal(stored, created.Message.VerifierMAC)
	}
	return store.Claim(f.t.Context(), f.store, record, parameters)
}

func (f *fixture) download(created *message, lease *store.Lease, now time.Time) error {
	record := f.get(created)
	return store.RecordDownload(f.t.Context(), f.store, record, lease.MAC, now)
}

func (f *fixture) ack(created *message, lease *store.Lease, now time.Time, blobs store.Blobs) error {
	record := f.get(created)
	return store.Ack(f.t.Context(), f.store, blobs, record, lease.MAC, now)
}

func (f *fixture) claimAndAck(created *message) {
	f.t.Helper()
	claimed, err := f.claim(created, baseTime, 1)
	if err != nil {
		f.t.Fatal(err)
	}
	if err = f.ack(created, &claimed.Lease, baseTime, f.blobs); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) end(created *message, state store.State, now time.Time, blobs store.Blobs) (store.State, error) {
	record := f.get(created)
	return store.End(f.t.Context(), f.store, blobs, record, state, now)
}

func (f *fixture) finish(created *message, now time.Time, blobs store.Blobs) (bool, error) {
	record := f.get(created)
	return store.Finish(f.t.Context(), f.store, blobs, record, now)
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

func (f *fixture) sweep(blobs store.Blobs, now time.Time) (store.SweepReport, error) {
	return store.SweepAll(f.t.Context(), f.store, blobs, now)
}

func newMessage(opens uint16, ttlSeconds uint32, passphrase bool) *store.Message {
	created := &store.Message{
		Version:        1,
		CreatedAt:      baseTime,
		ExpiresAt:      baseTime.Add(time.Duration(ttlSeconds) * time.Second),
		TTLSeconds:     ttlSeconds,
		OpensMax:       opens,
		OpensRemaining: opens,
		Size:           blobSize,
	}
	if passphrase {
		created.KDF = 1
		created.VerifierMAC = mac(fmt.Sprintf("verifier/%d", time.Now().UnixNano()))
		created.VerifierPepperID = 7
	}
	return created
}

func claimParams(record *store.Record, now time.Time, seed int) store.ClaimParameters {
	return store.ClaimParameters{
		Clock:         func() time.Time { return now },
		LeaseMAC:      mac(fmt.Sprintf("lease/%x/%d/%d", record.Row.Locator, now.UnixNano(), seed)),
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
