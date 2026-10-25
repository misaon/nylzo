package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const leaseInsertAttempts = 4

var (
	errClaimParams  = errors.New("sqlite: claim needs a clock, and a verifier check when a verifier is sent")
	errNoLeaseRowID = errors.New("sqlite: no free lease row id")
)

func (s *Store) Claim(ctx context.Context, parameters store.ClaimParams) (store.ClaimResult, error) {
	if parameters.Clock == nil || (parameters.VerifierSent && parameters.VerifyMAC == nil) {
		return store.ClaimResult{}, errClaimParams
	}
	var result store.ClaimResult
	var refusal *store.PassphraseError
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		now := parameters.Clock()
		message, err := s.claimable(ctx, tx, parameters.ID, now)
		if err != nil {
			return err
		}
		if message.PassphraseRequired() {
			err = checkPassphrase(ctx, tx, message, &parameters, now)
			if refused, ok := errors.AsType[*store.PassphraseError](err); ok {
				refusal = refused
				return nil
			}
			if err != nil {
				return err
			}
		}
		result, err = consumeOpen(ctx, tx, message, &parameters, now)
		return err
	})
	if err != nil {
		return store.ClaimResult{}, err
	}
	if refusal != nil {
		return store.ClaimResult{}, refusal
	}
	return result, nil
}

func (s *Store) claimable(ctx context.Context, tx *sql.Tx, id string, now time.Time) (*store.Message, error) {
	record, err := s.recordIn(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if record.Tombstone != nil {
		return nil, &store.GoneError{State: record.Tombstone.State}
	}
	message := record.Message
	switch {
	case message.Ending != "":
		return nil, &store.GoneError{State: message.Ending}
	case message.State != store.StateAvailable:
		return nil, &store.GoneError{State: message.State}
	case message.OpensRemaining == 0:
		return nil, &store.GoneError{State: store.StateOpened}
	case !now.Before(message.ExpiresAt):
		return nil, &store.GoneError{State: store.StateExpired}
	default:
		return message, nil
	}
}

func checkPassphrase(
	ctx context.Context,
	tx *sql.Tx,
	message *store.Message,
	parameters *store.ClaimParams,
	now time.Time,
) error {
	if message.FailCountTotal >= store.MaxFailedAttempts {
		return &store.PassphraseError{RetryAfter: message.ExpiresAt.Sub(now)}
	}
	if message.NextAttemptAt.After(now) {
		return &store.PassphraseError{RetryAfter: message.NextAttemptAt.Sub(now)}
	}
	if parameters.VerifierSent && parameters.VerifyMAC(message.PepperID, message.VerifierMAC) {
		return nil
	}
	failures := message.FailCountTotal + 1
	wait := backoff(failures)
	var next time.Time
	if wait > 0 {
		next = store.CeilMinute(now.Add(wait))
		wait = next.Sub(now)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE messages SET fail_count_total = ?, next_attempt_at = ? WHERE id = ?`,
		failures,
		attemptMilliseconds(next),
		message.ID,
	); err != nil {
		return err
	}
	return &store.PassphraseError{RetryAfter: wait, AttemptsLeft: max(0, store.FreeAttempts-failures)}
}

func consumeOpen(
	ctx context.Context,
	tx *sql.Tx,
	message *store.Message,
	parameters *store.ClaimParams,
	now time.Time,
) (store.ClaimResult, error) {
	message.OpensRemaining--
	if message.OpensRemaining == 0 {
		message.State = store.StateOpened
	}
	if message.FirstOpenedAt == nil {
		message.FirstOpenedAt = new(now.UTC().Truncate(time.Minute))
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE messages SET opens_remaining = ?, state = ?, first_opened_at = ? WHERE id = ?`,
		message.OpensRemaining,
		string(message.State),
		milliseconds(*message.FirstOpenedAt),
		message.ID,
	); err != nil {
		return store.ClaimResult{}, err
	}
	expires := store.CeilMinute(now.Add(store.LeaseFor(message.Size)))
	rowID, err := insertLease(ctx, tx, message.ID, parameters, expires)
	if err != nil {
		return store.ClaimResult{}, err
	}
	return store.ClaimResult{
		Message: message,
		Lease: store.Lease{
			RowID:     rowID,
			ID:        message.ID,
			MAC:       parameters.LeaseMAC,
			PepperID:  parameters.LeasePepperID,
			ExpiresAt: expires,
		},
	}, nil
}

func insertLease(
	ctx context.Context,
	tx *sql.Tx,
	id string,
	parameters *store.ClaimParams,
	expires time.Time,
) (int64, error) {
	for range leaseInsertAttempts {
		rowID := randomLeaseRowID()
		inserted, err := tx.ExecContext(
			ctx,
			`INSERT INTO leases (row_id, id, lease_mac, pepper_id, lease_expires_at, downloads) VALUES (?, ?, ?, ?, ?, 0) ON CONFLICT(row_id) DO NOTHING`,
			rowID,
			id,
			parameters.LeaseMAC,
			parameters.LeasePepperID,
			milliseconds(expires),
		)
		if err != nil {
			return 0, err
		}
		if affected, countErr := inserted.RowsAffected(); countErr != nil || affected == 1 {
			return rowID, countErr
		}
	}
	return 0, errNoLeaseRowID
}

func randomLeaseRowID() int64 {
	var random [8]byte
	for {
		rand.Read(random[:])
		if id := int64(binary.BigEndian.Uint64(random[:]) >> 1); id >= minLeaseRowID {
			return id
		}
	}
}

func backoff(failures int) time.Duration {
	if failures <= store.FreeAttempts {
		return 0
	}
	return min(store.MaxBackoff, time.Second<<min(failures-store.FreeAttempts, 20))
}

func attemptMilliseconds(moment time.Time) int64 {
	if moment.IsZero() {
		return 0
	}
	return milliseconds(moment)
}
