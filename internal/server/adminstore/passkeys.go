package adminstore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	"github.com/misaon/nylzo/internal/server/passkey"
)

// MaxPasskeys bounds what one admin area holds: the operator's devices and a spare key or two.
const MaxPasskeys = 8

var (
	// ErrEnrollmentClosed answers a token that was never issued, was used, or ran out.
	ErrEnrollmentClosed = errors.New("adminstore: no enrollment is open for this token")
	ErrTooManyPasskeys  = errors.New("adminstore: the admin area holds as many passkeys as it accepts")
	ErrPasskeyEnrolled  = errors.New("adminstore: the instance already holds this passkey")
)

type TokenHash [sha256.Size]byte

func HashToken(token []byte) TokenHash {
	return sha256.Sum256(token)
}

type Passkey struct {
	passkey.Credential

	CreatedAt  time.Time
	LastUsedAt time.Time
}

const storedColumns = `credential_id, public_key, algorithm, sign_count, backup_eligible, backed_up, created_at,
	last_used_at`

// OpenEnrollment replaces the enrollment that was open, so only the newest token of the operator works.
func (s *Store) OpenEnrollment(ctx context.Context, token TokenHash, expiresAt time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO enrollment (slot, token_hash, expires_at) VALUES (1, ?, ?)
				ON CONFLICT(slot) DO UPDATE SET token_hash = excluded.token_hash, expires_at = excluded.expires_at`,
			token[:],
			expiresAt.Unix(),
		)
		return err
	})
}

func (s *Store) EnrollmentOpen(ctx context.Context, token TokenHash, now time.Time) (bool, error) {
	var open bool
	err := inTransaction(ctx, s.database, func(tx *sql.Tx) error {
		var err error
		open, err = enrollmentOpen(ctx, tx, token, now)
		return err
	})
	return open, err
}

func enrollmentOpen(ctx context.Context, tx *sql.Tx, token TokenHash, now time.Time) (bool, error) {
	var (
		stored    []byte
		expiresAt int64
	)
	err := tx.QueryRowContext(ctx, `SELECT token_hash, expires_at FROM enrollment WHERE slot = 1`).
		Scan(&stored, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(stored, token[:]) == 1 && now.Unix() < expiresAt, nil
}

// Enroll stores the passkey and closes the enrollment in one transaction, so a token enrolls one passkey.
func (s *Store) Enroll(ctx context.Context, token TokenHash, credential *passkey.Credential, now time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		open, err := enrollmentOpen(ctx, tx, token, now)
		if err != nil {
			return err
		}
		if !open {
			return ErrEnrollmentClosed
		}
		var held int
		if countErr := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM passkeys`).Scan(&held); countErr != nil {
			return countErr
		}
		if held >= MaxPasskeys {
			return ErrTooManyPasskeys
		}
		var enrolled bool
		if existsErr := tx.QueryRowContext(
			ctx,
			`SELECT EXISTS (SELECT 1 FROM passkeys WHERE credential_id = ?)`,
			credential.ID,
		).Scan(&enrolled); existsErr != nil {
			return existsErr
		}
		if enrolled {
			return ErrPasskeyEnrolled
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM enrollment`); err != nil {
			return err
		}
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO passkeys (`+storedColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
			credential.ID,
			credential.PublicKey,
			int(credential.Algorithm),
			credential.SignCount,
			credential.BackupEligible,
			credential.BackedUp,
			now.Unix(),
		)
		return err
	})
}

func (s *Store) Passkey(ctx context.Context, credentialID []byte) (Passkey, bool, error) {
	row := s.database.QueryRowContext(
		ctx,
		`SELECT `+storedColumns+` FROM passkeys WHERE credential_id = ?`,
		credentialID,
	)
	found, err := scanPasskey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Passkey{}, false, nil
	}
	return found, err == nil, err
}

func (s *Store) Passkeys(ctx context.Context) ([]Passkey, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT `+storedColumns+` FROM passkeys ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []Passkey
	for rows.Next() {
		found, scanErr := scanPasskey(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		all = append(all, found)
	}
	return all, rows.Err()
}

func (s *Store) CountPasskeys(ctx context.Context) (int, error) {
	var held int
	err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM passkeys`).Scan(&held)
	return held, err
}

// RecordUse keeps the sign count and backup state the authenticator reported with a verified assertion. It reports
// false when the passkey is gone or another sign-in has moved its count as far already, so two assertions that
// were checked against the same count cannot both sign in.
func (s *Store) RecordUse(ctx context.Context, credential *passkey.Credential, usedAt time.Time) (bool, error) {
	var changed int64
	err := s.write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE passkeys SET sign_count = ?, backup_eligible = ?, backed_up = ?, last_used_at = ?
				WHERE credential_id = ? AND (sign_count < ? OR (sign_count = 0 AND ? = 0))`,
			credential.SignCount,
			credential.BackupEligible,
			credential.BackedUp,
			usedAt.Unix(),
			credential.ID,
			credential.SignCount,
			credential.SignCount,
		)
		if err != nil {
			return err
		}
		changed, err = result.RowsAffected()
		return err
	})
	return changed == 1, err
}

// RemovePasskeys also closes an open enrollment, so nothing of the old access is left.
func (s *Store) RemovePasskeys(ctx context.Context) (int64, error) {
	var removed int64
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM enrollment`); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM passkeys`)
		if err != nil {
			return err
		}
		removed, err = result.RowsAffected()
		return err
	})
	return removed, err
}

type scanner interface {
	Scan(destinations ...any) error
}

func scanPasskey(row scanner) (Passkey, error) {
	var (
		found               Passkey
		algorithm           int
		createdAt, lastUsed int64
	)
	err := row.Scan(
		&found.ID,
		&found.PublicKey,
		&algorithm,
		&found.SignCount,
		&found.BackupEligible,
		&found.BackedUp,
		&createdAt,
		&lastUsed,
	)
	found.Algorithm = passkey.Algorithm(algorithm)
	found.CreatedAt = time.Unix(createdAt, 0)
	if lastUsed > 0 {
		found.LastUsedAt = time.Unix(lastUsed, 0)
	}
	return found, err
}
