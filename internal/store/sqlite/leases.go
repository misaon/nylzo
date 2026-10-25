package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const (
	leasesQuery = `SELECT row_id, id, lease_mac, pepper_id, lease_expires_at, downloads FROM leases WHERE id = ? ORDER BY row_id`
	leaseQuery  = `SELECT l.lease_expires_at, l.downloads, m.ending FROM leases l JOIN messages m ON m.id = l.id WHERE l.row_id = ? AND l.id = ? AND l.lease_mac = ?`
)

func (s *Store) Leases(ctx context.Context, id string) ([]store.Lease, error) {
	rows, err := s.statements.leases.QueryContext(ctx, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var leases []store.Lease
	for rows.Next() {
		var lease store.Lease
		var pepperID, expires int64
		scanErr := rows.Scan(&lease.RowID, &lease.ID, &lease.MAC, &pepperID, &expires, &lease.Downloads)
		if scanErr != nil {
			return nil, scanErr
		}
		if narrowErr := narrow(&lease.PepperID, "pepper_id", pepperID); narrowErr != nil {
			return nil, narrowErr
		}
		lease.ExpiresAt = fromMilliseconds(expires)
		leases = append(leases, lease)
	}
	return leases, rows.Err()
}

func (s *Store) RecordDownload(ctx context.Context, lease *store.Lease, now time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		found, err := findLease(ctx, tx, lease)
		if err == nil && found.expiresAt <= milliseconds(now) {
			return store.ErrLeaseInvalid
		}
		if err != nil {
			return err
		}
		if found.ending.Valid {
			return &store.GoneError{State: store.State(found.ending.String)}
		}
		if found.downloads >= store.MaxLeaseDownload {
			return store.ErrLeaseLimit
		}
		_, err = tx.ExecContext(
			ctx,
			`UPDATE leases SET downloads = ? WHERE row_id = ?`,
			found.downloads+1,
			lease.RowID,
		)
		return err
	})
}

func (s *Store) Ack(ctx context.Context, lease *store.Lease, now time.Time) (string, error) {
	var ref string
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		found, err := findLease(ctx, tx, lease)
		if err != nil {
			return err
		}
		if found.ending.Valid {
			return &store.GoneError{State: store.State(found.ending.String)}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM leases WHERE row_id = ?`, lease.RowID); err != nil {
			return err
		}
		ref, err = releaseInTx(ctx, tx, lease.ID, now)
		return err
	})
	return ref, err
}

func (s *Store) ReleaseIfDone(ctx context.Context, id string, now time.Time) (string, error) {
	var ref string
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		ref, err = releaseInTx(ctx, tx, id, now)
		return err
	})
	return ref, err
}

func (s *Store) ClearBlob(ctx context.Context, id, blobRef string, now time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		record, err := s.recordIn(ctx, tx, id)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if record.Message == nil || record.Message.BlobRef != blobRef {
			return nil
		}
		return endInTx(ctx, tx, record.Message, endingOr(record.Message, store.StateOpened), now)
	})
}

type leaseRow struct {
	expiresAt int64
	downloads int
	ending    sql.NullString
}

func findLease(ctx context.Context, tx *sql.Tx, lease *store.Lease) (leaseRow, error) {
	var found leaseRow
	err := tx.QueryRowContext(ctx, leaseQuery, lease.RowID, lease.ID, lease.MAC).
		Scan(&found.expiresAt, &found.downloads, &found.ending)
	if errors.Is(err, sql.ErrNoRows) {
		return found, store.ErrLeaseInvalid
	}
	return found, err
}

func releaseInTx(ctx context.Context, tx *sql.Tx, id string, now time.Time) (string, error) {
	var opensRemaining int
	var blobRef sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT opens_remaining, blob_ref FROM messages WHERE id = ?`, id).
		Scan(&opensRemaining, &blobRef)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil || opensRemaining != 0 || blobRef.String == "" {
		return "", err
	}
	var active int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM leases WHERE id = ? AND lease_expires_at > ?`, id, milliseconds(now)).
		Scan(&active)
	if err != nil || active > 0 {
		return "", err
	}
	_, err = tx.ExecContext(
		ctx,
		`UPDATE messages SET ending = ? WHERE id = ? AND ending IS NULL AND expires_at > ?`,
		string(store.StateOpened),
		id,
		milliseconds(now),
	)
	return blobRef.String, err
}
