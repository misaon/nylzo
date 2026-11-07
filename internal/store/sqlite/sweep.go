package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

const (
	shredBlob  = `UPDATE messages SET blob_wrap = NULL WHERE locator = ? AND blob_ref = ?`
	forgetBlob = `UPDATE messages SET blob_ref = NULL, blob_wrap = NULL, blob_purge_at = NULL
		WHERE locator = ? AND blob_ref = ?`
)

var dueBlobsQuery = fmt.Sprintf(
	`SELECT locator, blob_ref FROM messages WHERE blob_ref IS NOT NULL AND blob_purge_at <= ? AND locator > ?
		AND length(locator) = %d ORDER BY locator LIMIT ?`,
	seal.LocatorLen,
)

func (s *Store) DueBlobs(ctx context.Context, now time.Time, after seal.Locator, limit int) ([]store.DueBlob, error) {
	rows, err := s.reader.QueryContext(
		ctx,
		dueBlobsQuery,
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
	return s.eachDue(ctx, s.statements.shredBlob, due)
}

func (s *Store) ForgetBlobs(ctx context.Context, due []store.DueBlob) error {
	return s.eachDue(ctx, s.statements.forgetBlob, due)
}

func (s *Store) eachDue(ctx context.Context, prepared *sql.Stmt, due []store.DueBlob) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		statement := tx.StmtContext(ctx, prepared)
		defer statement.Close()
		for _, blob := range due {
			if _, err := statement.ExecContext(ctx, blob.Locator[:], blob.Ref); err != nil {
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

func (s *Store) purge(ctx context.Context, query string, now time.Time, limit int) (int, error) {
	var purged int64
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, query, milliseconds(now), limit)
		if err != nil {
			return err
		}
		purged, err = result.RowsAffected()
		return err
	})
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
