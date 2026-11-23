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
		now.UnixMilli(),
		after[:],
		limit,
	)
	if err != nil {
		return nil, err
	}
	return readRows(rows, func(rows *sql.Rows) (store.DueBlob, error) {
		var due store.DueBlob
		var locator []byte
		scanErr := rows.Scan(&locator, &due.Reference)
		copy(due.Locator[:], locator)
		return due, scanErr
	})
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
			if _, err := statement.ExecContext(ctx, blob.Locator[:], blob.Reference); err != nil {
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

func (s *Store) CountBlobReferences(ctx context.Context) (int, error) {
	var named int
	err := s.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE blob_ref IS NOT NULL`).Scan(&named)
	return named, err
}

func (s *Store) KnownBlobReferences(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT blob_ref FROM messages WHERE blob_ref IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	references, err := readRows(rows, func(rows *sql.Rows) (string, error) {
		var reference string
		scanErr := rows.Scan(&reference)
		return reference, scanErr
	})
	known := make(map[string]struct{}, len(references))
	for _, reference := range references {
		known[reference] = struct{}{}
	}
	return known, err
}

func readRows[Value any](rows *sql.Rows, read func(rows *sql.Rows) (Value, error)) ([]Value, error) {
	defer rows.Close()
	var values []Value
	for rows.Next() {
		value, err := read(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) purge(ctx context.Context, query string, now time.Time, limit int) (int, error) {
	var purged int64
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, query, now.UnixMilli(), limit)
		if err != nil {
			return err
		}
		purged, err = result.RowsAffected()
		return err
	})
	return int(purged), err
}
