package store

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

var errClaimParameters = errors.New("store: claim needs a clock")

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

// Claim spends an open of a message whose handshake the caller has finished with a matching KE3.
func Claim(ctx context.Context, messages Messages, record *Record, parameters ClaimParameters) (ClaimResult, error) {
	if parameters.Clock == nil {
		return ClaimResult{}, errClaimParameters
	}
	var result ClaimResult
	err := record.update(ctx, messages, parameters.Clock(), func(current *Record) error {
		now := parameters.Clock()
		result = ClaimResult{}
		if err := claimClosed(current, now); err != nil {
			return err
		}
		result = ClaimResult{Message: current.Message, Lease: consumeOpen(current, &parameters, now)}
		return nil
	})
	if err != nil {
		return ClaimResult{}, err
	}
	return result, nil
}

// CountAttempt counts a handshake as a failed passphrase attempt until a matching KE3 takes it back, because its KE2
// lets the client test one passphrase (spec section 6.4, RFC 9807 section 9.2). A refusal counts nothing. It also runs
// on a message whose last open is spent while a lease is active, which a burn and a resumed download need.
func CountAttempt(
	ctx context.Context,
	messages Messages,
	record *Record,
	clock func() time.Time,
) (Attempt, PassphraseError, error) {
	var attempt Attempt
	var status PassphraseError
	var refusal *PassphraseError
	err := record.update(ctx, messages, clock(), func(current *Record) error {
		now := clock()
		attempt, status, refusal = Attempt{}, PassphraseError{}, nil
		if current.Ended(now) {
			return &GoneError{State: current.State(now)}
		}
		message := current.Message
		if locked, refused := lockedOut(message, now); refused {
			refusal = &locked
			return nil
		}
		attempt.Previous = message.NextAttemptAt
		message.FailCountTotal++
		message.NextAttemptAt = time.Time{}
		if wait := backoff(message.FailCountTotal); wait > 0 {
			message.NextAttemptAt = CeilMinute(now.Add(wait))
		}
		attempt.Set = message.NextAttemptAt
		status = PassphraseStatus(message, now)
		return nil
	})
	switch {
	case err != nil:
		return Attempt{}, PassphraseError{}, err
	case refusal != nil:
		return Attempt{}, PassphraseError{}, refusal
	default:
		return attempt, status, nil
	}
}

// PassphraseStatus is what a reader is told after an attempt: the attempts left before waits and the current wait, or
// the lock until the expiry once the waits have run out.
func PassphraseStatus(message *Message, now time.Time) PassphraseError {
	status := PassphraseError{AttemptsLeft: max(0, FreeAttempts-message.FailCountTotal)}
	switch {
	case message.NextAttemptAt.After(now):
		status.RetryAfter = message.NextAttemptAt.Sub(now)
	case message.FailCountTotal >= MaxFailedAttempts:
		status.RetryAfter = max(0, message.ExpiresAt.Sub(now))
	default:
	}
	return status
}

// TakeBack undoes the count of a handshake whose KE3 matched, in a transaction of its own, so it stands whatever the
// claim or burn then answers.
func TakeBack(ctx context.Context, messages Messages, record *Record, attempt Attempt, now time.Time) error {
	return record.update(ctx, messages, now, func(current *Record) error {
		message := current.Message
		message.FailCountTotal = max(0, message.FailCountTotal-1)
		if message.NextAttemptAt.Equal(attempt.Set) {
			message.NextAttemptAt = attempt.Previous
		}
		return nil
	})
}

// RefuseAttempt answers a handshake that CountAttempt would refuse, so such a handshake reads no blob; the transaction
// decides again.
func RefuseAttempt(record *Record, now time.Time) error {
	if record.Ended(now) {
		return &GoneError{State: record.State(now)}
	}
	if refusal, refused := lockedOut(record.Message, now); refused {
		return &refusal
	}
	return nil
}

// RefuseClaim answers a claim that the transaction would refuse, so such a claim reads no blob; the transaction
// decides again. The handshake before it has already applied the passphrase waits.
func RefuseClaim(record *Record, now time.Time) error {
	return claimClosed(record, now)
}

func claimClosed(record *Record, now time.Time) error {
	if state := record.State(now); state != StateAvailable {
		return &GoneError{State: state}
	}
	if wait, busy := leasesFull(record.Message, now); busy {
		return &BusyError{RetryAfter: wait}
	}
	return nil
}

// lockedOut refuses an attempt after the lifetime cap or before next_attempt_at without testing the passphrase.
func lockedOut(message *Message, now time.Time) (PassphraseError, bool) {
	if message.FailCountTotal >= MaxFailedAttempts {
		return PassphraseError{RetryAfter: message.ExpiresAt.Sub(now)}, true
	}
	if message.NextAttemptAt.After(now) {
		return PassphraseError{RetryAfter: message.NextAttemptAt.Sub(now)}, true
	}
	return PassphraseError{}, false
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
		case current.Ended(now):
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

// MarkEnding records an ending unless the message has already ended, and reports its state.
func MarkEnding(ctx context.Context, messages Messages, record *Record, requested State, now time.Time) (State, error) {
	var final State
	err := record.update(ctx, messages, now, func(current *Record) error {
		final = current.State(now)
		if current.Ended(now) {
			return nil
		}
		final = requested
		current.Message.Ending = requested
		current.recordEnd(now)
		return nil
	})
	return final, err
}

// End records a cancel or a burn, whose KE3 the caller has checked (F05), then deletes the blob even when the caller
// has gone.
func End(
	ctx context.Context,
	messages Messages,
	blobs Blobs,
	record *Record,
	requested State,
	now time.Time,
) (State, error) {
	ctx = context.WithoutCancel(ctx)
	final, err := MarkEnding(ctx, messages, record, requested, now)
	switch {
	case err != nil:
		return "", err
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
