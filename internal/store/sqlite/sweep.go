package sqlite

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const checkpointMS = 250

var ErrCheckpointBusy = errors.New("sqlite: the WAL checkpoint could not finish because a reader held the log")

func (s *Store) ExpiredPage(
	ctx context.Context,
	now time.Time,
	after store.ExpiredMessage,
	limit int,
) ([]store.ExpiredMessage, error) {
	rows, err := s.reader.QueryContext(
		ctx,
		`SELECT id, blob_ref, expires_at FROM messages WHERE expires_at <= ? AND (expires_at, id) > (?, ?) ORDER BY expires_at, id LIMIT ?`,
		ms(now),
		ms(after.ExpiresAt),
		after.ID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var page []store.ExpiredMessage
	for rows.Next() {
		var expired store.ExpiredMessage
		var expiresAt int64
		var blobRef sql.NullString
		if scanErr := rows.Scan(&expired.ID, &blobRef, &expiresAt); scanErr != nil {
			return nil, scanErr
		}
		expired.BlobRef = blobRef.String
		expired.ExpiresAt = fromMS(expiresAt)
		page = append(page, expired)
	}
	return page, rows.Err()
}

func (s *Store) SpentPage(ctx context.Context, after string, limit int) ([]string, error) {
	return s.strings(
		ctx,
		`SELECT id FROM messages WHERE opens_remaining = 0 AND blob_ref IS NOT NULL AND id > ? ORDER BY id LIMIT ?`,
		after,
		limit,
	)
}

func (s *Store) EndingPage(ctx context.Context, now time.Time, after string, limit int) ([]string, error) {
	return s.strings(
		ctx,
		`SELECT id FROM messages WHERE ending IS NOT NULL AND expires_at > ? AND id > ? ORDER BY id LIMIT ?`,
		ms(now),
		after,
		limit,
	)
}

func (s *Store) KnownBlobRefs(ctx context.Context) (map[string]struct{}, error) {
	refs, err := s.strings(ctx, `SELECT blob_ref FROM messages WHERE blob_ref IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		known[ref] = struct{}{}
	}
	return known, nil
}

func (s *Store) PurgeLeases(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.purge(
		ctx,
		`DELETE FROM leases WHERE row_id IN (SELECT row_id FROM leases WHERE lease_expires_at <= ? LIMIT ?)`,
		now,
		limit,
	)
}

func (s *Store) PurgeTombstones(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.purge(
		ctx,
		`DELETE FROM tombstones WHERE id IN (SELECT id FROM tombstones WHERE purge_at <= ? LIMIT ?)`,
		now,
		limit,
	)
}

func (s *Store) Checkpoint(ctx context.Context) error {
	conn, err := s.writer.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA busy_timeout = %d`, checkpointMS)); err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	var blocked, frames, moved int
	err = conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&blocked, &frames, &moved)
	switch {
	case err != nil:
		err = fmt.Errorf("sqlite: wal checkpoint: %w", err)
	case blocked != 0:
		err = ErrCheckpointBusy
	default:
	}
	restore := fmt.Sprintf(`PRAGMA busy_timeout = %d`, busyMS)
	if _, restoreErr := conn.ExecContext(context.WithoutCancel(ctx), restore); restoreErr != nil {
		discarded := conn.Raw(func(any) error { return sqldriver.ErrBadConn })
		return errors.Join(err, fmt.Errorf("sqlite: wal checkpoint: %w", restoreErr), ignoreBadConn(discarded))
	}
	return err
}

func (s *Store) purge(ctx context.Context, query string, now time.Time, limit int) (int, error) {
	result, err := s.writer.ExecContext(ctx, query, ms(now), limit)
	if err != nil {
		return 0, err
	}
	purged, err := result.RowsAffected()
	return int(purged), err
}

func (s *Store) strings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if scanErr := rows.Scan(&value); scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func ignoreBadConn(err error) error {
	if errors.Is(err, sqldriver.ErrBadConn) {
		return nil
	}
	return err
}
