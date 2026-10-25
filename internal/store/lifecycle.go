package store

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

var errClaimParameters = errors.New("store: claim needs a clock, and a verifier check when a verifier is sent")

type Removal int

const (
	Removed Removal = iota
	AlreadyEnded
)

// update opens the row again inside the write transaction, so every change starts from the current record.
func (r *Record) update(ctx context.Context, messages Messages, change func(current *Record) error) error {
	return messages.Update(ctx, r.Row.Locator, func(row *Row) error {
		current, err := OpenRecord(row, r.Key)
		if err != nil {
			return err
		}
		if changeErr := change(&current); changeErr != nil {
			return changeErr
		}
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
	if parameters.Clock == nil || (parameters.VerifierSent && parameters.VerifyMAC == nil) {
		return ClaimResult{}, errClaimParameters
	}
	var result ClaimResult
	var refusal PassphraseError
	refused := false
	err := record.update(ctx, messages, func(current *Record) error {
		now := parameters.Clock()
		result, refused = ClaimResult{}, false
		if state := current.State(now); state != StateAvailable {
			return &GoneError{State: state}
		}
		if current.Message.PassphraseRequired() {
			if refusal, refused = checkPassphrase(current.Message, &parameters, now); refused {
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

// checkPassphrase commits a failed attempt with the claim transaction, so it reports a refusal instead of an error.
func checkPassphrase(message *Message, parameters *ClaimParameters, now time.Time) (PassphraseError, bool) {
	if message.FailCountTotal >= MaxFailedAttempts {
		return PassphraseError{RetryAfter: message.ExpiresAt.Sub(now)}, true
	}
	if message.NextAttemptAt.After(now) {
		return PassphraseError{RetryAfter: message.NextAttemptAt.Sub(now)}, true
	}
	if parameters.VerifierSent && parameters.VerifyMAC(message.VerifierPepperID, message.VerifierMAC) {
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

func consumeOpen(record *Record, parameters *ClaimParameters, now time.Time) Lease {
	message := record.Message
	message.OpensRemaining--
	if message.FirstOpenedAt == nil {
		message.FirstOpenedAt = new(now.UTC().Truncate(time.Minute))
	}
	message.Leases = slices.DeleteFunc(message.Leases, func(lease Lease) bool { return !lease.Active(now) })
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

func backoff(failures int) time.Duration {
	if failures <= FreeAttempts {
		return 0
	}
	return min(MaxBackoff, time.Second<<min(failures-FreeAttempts, 20))
}

func RecordDownload(ctx context.Context, messages Messages, record *Record, leaseMAC []byte, now time.Time) error {
	return record.update(ctx, messages, func(current *Record) error {
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
	err := record.update(ctx, messages, func(current *Record) error {
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
	return DeleteBlob(ctx, messages, blobs, record.Row.Locator, record.Row.BlobRef)
}

// MarkEnding records burn or cancel unless the message has already ended, and reports the state it ends in.
func MarkEnding(ctx context.Context, messages Messages, record *Record, requested State, now time.Time) (State, error) {
	var final State
	err := record.update(ctx, messages, func(current *Record) error {
		final = current.State(now)
		if current.Message.Ending != "" || !now.Before(current.Message.ExpiresAt) || !current.HasBlob() {
			return nil
		}
		final = requested
		current.Message.Ending = requested
		current.recordEnd(now)
		return nil
	})
	return final, err
}

// End records burn or cancel, then deletes the blob even when the caller has gone.
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
	if err != nil {
		return "", err
	}
	if record.Row.BlobRef != "" {
		err = DeleteBlob(ctx, messages, blobs, record.Row.Locator, record.Row.BlobRef)
	}
	return final, err
}

// Finish deletes the blob of a message that has ended or whose last lease is over; a release records its minute.
func Finish(ctx context.Context, messages Messages, blobs Blobs, record *Record, now time.Time) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	if record.Row.BlobRef == "" || (!record.Ended(now) && !record.Releasable(now)) {
		return false, nil
	}
	if !record.Ended(now) {
		err := record.update(ctx, messages, func(current *Record) error {
			if !current.Ended(now) && current.Releasable(now) {
				current.recordEnd(now)
			}
			return nil
		})
		if err != nil {
			return false, err
		}
	}
	if record.Row.BlobRef == "" {
		return false, nil
	}
	return true, DeleteBlob(ctx, messages, blobs, record.Row.Locator, record.Row.BlobRef)
}

// DeleteBlob makes the blob unreadable before it deletes it, so a failure in between leaves nothing to read.
func DeleteBlob(ctx context.Context, messages Messages, blobs Blobs, locator seal.Locator, ref string) error {
	ctx = context.WithoutCancel(ctx)
	due := []DueBlob{{Locator: locator, Ref: ref}}
	if err := messages.ShredBlobs(ctx, due); err != nil {
		return err
	}
	if err := blobs.Delete(ctx, ref); err != nil {
		return err
	}
	return messages.ForgetBlobs(ctx, due)
}

// CountReport takes one of the reports a message may get on a UTC day and reports false when they are used up.
func CountReport(ctx context.Context, messages Messages, record *Record, now time.Time) (bool, error) {
	counted := false
	err := record.update(ctx, messages, func(current *Record) error {
		counted = false
		if current.Ended(now) {
			return &GoneError{State: current.State(now)}
		}
		message := current.Message
		if today := reportDay(now); message.ReportDay != today {
			message.ReportDay, message.Reports = today, 0
		}
		if message.Reports >= ReportsPerDay {
			return nil
		}
		message.Reports++
		counted = true
		return nil
	})
	return counted, err
}

func UncountReport(ctx context.Context, messages Messages, record *Record, now time.Time) error {
	return record.update(context.WithoutCancel(ctx), messages, func(current *Record) error {
		if message := current.Message; message.ReportDay == reportDay(now) && message.Reports > 0 {
			message.Reports--
		}
		return nil
	})
}

// TakeDown removes a message for the operator, who has its id but no key to open its record.
func TakeDown(
	ctx context.Context,
	messages Messages,
	blobs Blobs,
	locator seal.Locator,
	now time.Time,
) (Removal, error) {
	ctx = context.WithoutCancel(ctx)
	removal := Removed
	var ref string
	err := messages.Update(ctx, locator, func(row *Row) error {
		ref = row.BlobRef
		if row.BlobWrap == nil {
			removal = AlreadyEnded
			return nil
		}
		row.BlobWrap = nil
		row.BlobPurgeAt = ceil(now, BlobPurgeStep)
		row.PurgeAt = earlier(row.PurgeAt, ceil(now.Add(TombstoneTTL), RowPurgeStep))
		return nil
	})
	if err != nil || ref == "" {
		return removal, err
	}
	return removal, DeleteBlob(ctx, messages, blobs, locator, ref)
}

type ReconcileReport struct {
	Orphans int
	Listed  bool
}

func Reconcile(ctx context.Context, messages Messages, blobs Blobs, now time.Time) (ReconcileReport, error) {
	known, err := messages.KnownBlobRefs(ctx)
	if err != nil {
		return ReconcileReport{}, err
	}
	objects, err := blobs.List(ctx)
	if err != nil {
		return ReconcileReport{}, err
	}
	cutoff := now.Add(-BlobGrace)
	orphans := 0
	var errs []error
	for _, object := range objects {
		if ctx.Err() != nil {
			break
		}
		if _, live := known[object.Ref]; live || object.Modified.After(cutoff) {
			continue
		}
		if err = blobs.Delete(ctx, object.Ref); err != nil {
			errs = append(errs, err)
			continue
		}
		orphans++
	}
	return ReconcileReport{Orphans: orphans, Listed: true}, errors.Join(errs...)
}
