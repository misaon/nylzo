package sqlite

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

const checkpointTimeoutMilliseconds = 250

var ErrCheckpointBusy = errors.New("sqlite: the WAL checkpoint could not finish because a reader held the log")

func (s *Store) DueBlobs(ctx context.Context, now time.Time, after seal.Locator, limit int) ([]store.DueBlob, error) {
	rows, err := s.reader.QueryContext(
		ctx,
		`SELECT locator, blob_ref FROM messages WHERE blob_ref IS NOT NULL AND blob_purge_at <= ? AND locator > ? AND length(locator) = 32 ORDER BY locator LIMIT ?`,
		milliseconds(now),
		after[:],
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var page []store.DueBlob
	for rows.Next() {
		var due store.DueBlob
		var locator []byte
		if scanErr := rows.Scan(&locator, &due.Ref); scanErr != nil {
			return nil, scanErr
		}
		due.Locator = seal.Locator(locator)
		page = append(page, due)
	}
	return page, rows.Err()
}

func (s *Store) ShredBlobs(ctx context.Context, due []store.DueBlob) error {
	return s.eachDue(ctx, `UPDATE messages SET blob_wrap = NULL WHERE locator = ? AND blob_ref = ?`, due)
}

func (s *Store) ForgetBlobs(ctx context.Context, due []store.DueBlob) error {
	return s.eachDue(
		ctx,
		`UPDATE messages SET blob_ref = NULL, blob_wrap = NULL, blob_purge_at = NULL WHERE locator = ? AND blob_ref = ?`,
		due,
	)
}

func (s *Store) eachDue(ctx context.Context, query string, due []store.DueBlob) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		for _, blob := range due {
			if _, err := tx.ExecContext(ctx, query, blob.Locator[:], blob.Ref); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) PurgeRows(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.purge(
		ctx,
		`DELETE FROM messages WHERE locator IN (SELECT locator FROM messages WHERE purge_at <= ? LIMIT ?)`,
		now,
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

func (s *Store) Checkpoint(ctx context.Context) error {
	connection, err := s.writer.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	defer connection.Close()
	if _, err = connection.ExecContext(
		ctx,
		fmt.Sprintf(`PRAGMA busy_timeout = %d`, checkpointTimeoutMilliseconds),
	); err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	var blocked, frames, moved int
	err = connection.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&blocked, &frames, &moved)
	switch {
	case err != nil:
		err = fmt.Errorf("sqlite: wal checkpoint: %w", err)
	case blocked != 0:
		err = ErrCheckpointBusy
	default:
	}
	restore := fmt.Sprintf(`PRAGMA busy_timeout = %d`, busyTimeoutMilliseconds)
	if _, restoreErr := connection.ExecContext(context.WithoutCancel(ctx), restore); restoreErr != nil {
		discarded := connection.Raw(func(any) error { return sqldriver.ErrBadConn })
		return errors.Join(err, fmt.Errorf("sqlite: wal checkpoint: %w", restoreErr), ignoreBadConn(discarded))
	}
	return err
}

func (s *Store) purge(ctx context.Context, query string, now time.Time, limit int) (int, error) {
	result, err := s.writer.ExecContext(ctx, query, milliseconds(now), limit)
	if err != nil {
		return 0, err
	}
	purged, err := result.RowsAffected()
	return int(purged), err
}

func (s *Store) strings(ctx context.Context, query string, arguments ...any) ([]string, error) {
	rows, err := s.reader.QueryContext(ctx, query, arguments...)
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
