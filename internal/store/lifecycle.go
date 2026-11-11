package store

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

var (
	errClaimParameters = errors.New("store: claim needs a clock, and a verifier check when a verifier is sent")
	errVerifierCheck   = errors.New("store: a sent verifier needs a check")
)

type Removal int

const (
	Removed Removal = iota
	AlreadyEnded
)

// update opens the row again inside the write transaction, so every change starts from the current record.
func (r *Record) update(
	ctx context.Context,
	messages Messages,
	now time.Time,
	change func(current *Record) error,
) error {
	return messages.Update(ctx, r.Row.Locator, func(row *Row) error {
		current, err := OpenRecord(row, r.Key)
		if err != nil {
			return err
		}
		if changeErr := change(&current); changeErr != nil {
			return changeErr
		}
		current.pruneLeases(now)
		current.reseal()
		*row = current.Row
		*r = current
		return nil
	})
}

func (r *Record) Lease(mac []byte) *Lease {
	for i := range r.Message.Leases {
		if bytes.Equal(r.Message.Leases[i].MAC, mac) {
			return &r.Message.Leases[i]
		}
	}
	return nil
}

func Claim(ctx context.Context, messages Messages, record *Record, parameters ClaimParameters) (ClaimResult, error) {
	if parameters.Clock == nil || !parameters.Verifier.checkable() {
		return ClaimResult{}, errClaimParameters
	}
	var result ClaimResult
	var refusal PassphraseError
	refused := false
	err := record.update(ctx, messages, parameters.Clock(), func(current *Record) error {
		now := parameters.Clock()
		result, refused = ClaimResult{}, false
		if state := current.State(now); state != StateAvailable {
			return &GoneError{State: state}
		}
		if wait, busy := leasesFull(current.Message, now); busy {
			return &BusyError{RetryAfter: wait}
		}
		if current.Message.PassphraseRequired() {
			if refusal, refused = checkPassphrase(current.Message, &parameters.Verifier, now); refused {
				return nil
			}
		}
		result = ClaimResult{Message: current.Message, Lease: consumeOpen(current, &parameters, now)}
		return nil
	})
	switch {
	case err != nil:
		return ClaimResult{}, err
	case refused:
		return ClaimResult{}, &refusal
	default:
		return result, nil
	}
}

func (v *Verifier) checkable() bool {
	return !v.Sent || v.Check != nil
}

// checkPassphrase commits a failed attempt with the claim or burn transaction, so it reports a refusal, not an error.
func checkPassphrase(message *Message, verifier *Verifier, now time.Time) (PassphraseError, bool) {
	if message.FailCountTotal >= MaxFailedAttempts {
		return PassphraseError{RetryAfter: message.ExpiresAt.Sub(now)}, true
	}
	if message.NextAttemptAt.After(now) {
		return PassphraseError{RetryAfter: message.NextAttemptAt.Sub(now)}, true
	}
	if verifier.Sent && verifier.Check(message.VerifierPepperID, message.VerifierMAC) {
		return PassphraseError{}, false
	}
	message.FailCountTotal++
	message.NextAttemptAt = time.Time{}
	wait := backoff(message.FailCountTotal)
	if wait > 0 {
		message.NextAttemptAt = CeilMinute(now.Add(wait))
		wait = message.NextAttemptAt.Sub(now)
	}
	return PassphraseError{RetryAfter: wait, AttemptsLeft: max(0, FreeAttempts-message.FailCountTotal)}, true
}

// leasesFull caps the active leases, so every record fits one RecordBlock and its length says nothing (A4-4).
func leasesFull(message *Message, now time.Time) (time.Duration, bool) {
	var active []time.Time
	for _, lease := range message.Leases {
		if lease.Active(now) {
			active = append(active, lease.ExpiresAt)
		}
	}
	if len(active) < MaxActiveLeases {
		return 0, false
	}
	return slices.MinFunc(active, time.Time.Compare).Sub(now), true
}

func consumeOpen(record *Record, parameters *ClaimParameters, now time.Time) Lease {
	message := record.Message
	message.OpensRemaining--
	if message.FirstOpenedAt == nil {
		message.FirstOpenedAt = new(now.UTC().Truncate(time.Minute))
	}
	record.pruneLeases(now)
	lease := Lease{
		MAC:       parameters.LeaseMAC,
		PepperID:  parameters.LeasePepperID,
		ExpiresAt: CeilMinute(now.Add(LeaseFor(message.Size))),
	}
	message.Leases = append(message.Leases, lease)
	if message.OpensRemaining == 0 {
		end := earlier(lastLeaseEnd(message), message.ExpiresAt)
		record.Row.BlobPurgeAt = ceil(end, BlobPurgeStep)
		record.Row.PurgeAt = ceil(end.Add(TombstoneTTL), RowPurgeStep)
	}
	return lease
}

// The cap keeps the shift far from overflowing a Duration; MaxBackoff bounds the result long before it.
const maxBackoffDoublings = 20

func backoff(failures int) time.Duration {
	if failures <= FreeAttempts {
		return 0
	}
	return min(MaxBackoff, time.Second<<min(failures-FreeAttempts, maxBackoffDoublings))
}

func RecordDownload(ctx context.Context, messages Messages, record *Record, leaseMAC []byte, now time.Time) error {
	return record.update(ctx, messages, now, func(current *Record) error {
		lease := current.Lease(leaseMAC)
		switch {
		case lease == nil || !lease.Active(now):
			return ErrLeaseInvalid
		case current.Message.Ending != "" || !current.HasBlob():
			return &GoneError{State: current.State(now)}
		case lease.Downloads >= MaxLeaseDownload:
			return ErrLeaseLimit
		default:
			lease.Downloads++
			return nil
		}
	})
}

// Ack ends a lease and deletes the blob when it was the last one.
func Ack(ctx context.Context, messages Messages, blobs Blobs, record *Record, leaseMAC []byte, now time.Time) error {
	release := false
	err := record.update(ctx, messages, now, func(current *Record) error {
		switch {
		case current.Lease(leaseMAC) == nil:
			return ErrLeaseInvalid
		case current.Message.Ending != "" || !current.HasBlob():
			return &GoneError{State: current.State(now)}
		}
		current.Message.Leases = slices.DeleteFunc(current.Message.Leases, func(lease Lease) bool {
			return bytes.Equal(lease.MAC, leaseMAC)
		})
		release = current.Releasable(now)
		if release {
			current.recordEnd(now)
		}
		return nil
	})
	if err != nil || !release {
		return err
	}
	return deleteBlobOf(ctx, messages, blobs, record)
}

// MarkEnding records an ending that needs no passphrase unless the message has already ended, and reports its state.
func MarkEnding(ctx context.Context, messages Messages, record *Record, requested State, now time.Time) (State, error) {
	final, _, err := recordEnding(ctx, messages, record, requested, now, nil)
	return final, err
}

// recordEnding checks the passphrase first when a verifier comes with the request, and a refusal commits the failed
// attempt instead of the ending.
func recordEnding(
	ctx context.Context,
	messages Messages,
	record *Record,
	requested State,
	now time.Time,
	verifier *Verifier,
) (State, *PassphraseError, error) {
	var final State
	var refusal *PassphraseError
	err := record.update(ctx, messages, now, func(current *Record) error {
		final, refusal = current.State(now), nil
		if current.Ended(now) {
			return nil
		}
		if verifier != nil && current.Message.PassphraseRequired() {
			if refused, ok := checkPassphrase(current.Message, verifier, now); ok {
				refusal = &refused
				return nil
			}
		}
		final = requested
		current.Message.Ending = requested
		current.recordEnd(now)
		return nil
	})
	return final, refusal, err
}

// End records a cancel or another ending that needs no passphrase, then deletes the blob even when the caller has gone.
func End(
	ctx context.Context,
	messages Messages,
	blobs Blobs,
	record *Record,
	requested State,
	now time.Time,
) (State, error) {
	return endMessage(ctx, messages, blobs, record, requested, now, nil)
}

// Burn ends a message with a passphrase only for a request that proves it, counting a mismatch as a claim does (F05).
func Burn(
	ctx context.Context,
	messages Messages,
	blobs Blobs,
	record *Record,
	verifier Verifier,
	now time.Time,
) (State, error) {
	if !verifier.checkable() {
		return "", errVerifierCheck
	}
	return endMessage(ctx, messages, blobs, record, StateDeleted, now, &verifier)
}

func endMessage(
	ctx context.Context,
	messages Messages,
	blobs Blobs,
	record *Record,
	requested State,
	now time.Time,
	verifier *Verifier,
) (State, error) {
	ctx = context.WithoutCancel(ctx)
	final, refusal, err := recordEnding(ctx, messages, record, requested, now, verifier)
	switch {
	case err != nil:
		return "", err
	case refusal != nil:
		return "", refusal
	case record.Row.BlobReference == "":
		return final, nil
	default:
		return final, deleteBlobOf(ctx, messages, blobs, record)
	}
}

// Finish deletes the blob of a message that has ended or whose last lease is over; a release records its minute.
func Finish(ctx context.Context, messages Messages, blobs Blobs, record *Record, now time.Time) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	if record.Row.BlobReference == "" || (!record.Ended(now) && !record.Releasable(now)) {
		return false, nil
	}
	if !record.Ended(now) {
		err := record.update(ctx, messages, now, func(current *Record) error {
			if !current.Ended(now) && current.Releasable(now) {
				current.recordEnd(now)
			}
			return nil
		})
		if err != nil {
			return false, err
		}
	}
	if record.Row.BlobReference == "" {
		return false, nil
	}
	return true, deleteBlobOf(ctx, messages, blobs, record)
}

// DeleteBlob makes the blob unreadable before it deletes it, so a failure in between leaves nothing to read.
func DeleteBlob(ctx context.Context, messages Messages, blobs Blobs, locator seal.Locator, reference string) error {
	ctx = context.WithoutCancel(ctx)
	if err := messages.ShredBlobs(ctx, []DueBlob{{Locator: locator, Reference: reference}}); err != nil {
		return err
	}
	return forgetBlob(ctx, messages, blobs, locator, reference)
}

// deleteBlobOf skips the shred when the record already holds no blob wrap, which no write ever restores.
func deleteBlobOf(ctx context.Context, messages Messages, blobs Blobs, record *Record) error {
	if record.HasBlob() {
		return DeleteBlob(ctx, messages, blobs, record.Row.Locator, record.Row.BlobReference)
	}
	return forgetBlob(context.WithoutCancel(ctx), messages, blobs, record.Row.Locator, record.Row.BlobReference)
}

func forgetBlob(ctx context.Context, messages Messages, blobs Blobs, locator seal.Locator, reference string) error {
	if err := blobs.Delete(ctx, reference); err != nil {
		return err
	}
	return messages.ForgetBlobs(ctx, []DueBlob{{Locator: locator, Reference: reference}})
}

// CountReport takes one of the reports a message may get over its life and reports false when they are used up, so
// the sender of many messages cannot fill the report files with reports about them (F6).
func CountReport(ctx context.Context, messages Messages, record *Record, now time.Time) (bool, error) {
	counted := false
	err := record.update(ctx, messages, now, func(current *Record) error {
		counted = false
		if current.Ended(now) {
			return &GoneError{State: current.State(now)}
		}
		message := current.Message
		if message.Reports >= ReportsPerMessage {
			return nil
		}
		message.Reports++
		counted = true
		return nil
	})
	return counted, err
}

func UncountReport(ctx context.Context, messages Messages, record *Record, now time.Time) error {
	return record.update(context.WithoutCancel(ctx), messages, now, func(current *Record) error {
		if message := current.Message; message.Reports > 0 {
			message.Reports--
		}
		return nil
	})
}

// TakeDown removes a message for the operator, who has its id but no key; a blob already due for the sweep counts as ended.
func TakeDown(
	ctx context.Context,
	messages Messages,
	blobs Blobs,
	locator seal.Locator,
	now time.Time,
) (Removal, error) {
	ctx = context.WithoutCancel(ctx)
	removal := Removed
	var reference string
	err := messages.Update(ctx, locator, func(row *Row) error {
		reference = row.BlobReference
		if row.BlobWrap == nil {
			removal = AlreadyEnded
			return nil
		}
		if !row.BlobPurgeAt.IsZero() && !now.Before(row.BlobPurgeAt) {
			removal = AlreadyEnded
		}
		row.BlobWrap = nil
		row.BlobPurgeAt = ceil(now, BlobPurgeStep)
		row.PurgeAt = earlier(row.PurgeAt, ceil(now.Add(TombstoneTTL), RowPurgeStep))
		return nil
	})
	if err != nil || reference == "" {
		return removal, err
	}
	return removal, forgetBlob(ctx, messages, blobs, locator, reference)
}

type ReconcileReport struct {
	Orphans int
	Listed  bool
}

// Reconcile deletes a blob that no row holds once the blob_purge_at it carries has passed, so an upload whose row is
// still being written stays, and an orphan goes when its message would have expired.
func Reconcile(ctx context.Context, messages Messages, blobs Blobs, now time.Time) (ReconcileReport, error) {
	known, err := messages.KnownBlobReferences(ctx)
	if err != nil {
		return ReconcileReport{}, err
	}
	objects, err := blobs.List(ctx)
	if err != nil {
		return ReconcileReport{}, err
	}
	orphans := 0
	var errs []error
	for _, object := range objects {
		if ctx.Err() != nil {
			break
		}
		if _, live := known[object.Reference]; live || object.PurgeAt.After(now) {
			continue
		}
		if err = blobs.Delete(ctx, object.Reference); err != nil {
			errs = append(errs, err)
			continue
		}
		orphans++
	}
	return ReconcileReport{Orphans: orphans, Listed: true}, errors.Join(errs...)
}
