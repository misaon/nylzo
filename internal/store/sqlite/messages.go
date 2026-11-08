package sqlite

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

const (
	insertRow = `INSERT INTO messages (locator, read_wrap, manage_wrap, record, blob_ref, blob_wrap, blob_purge_at,
		purge_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(locator) DO NOTHING`
	updateRow = `UPDATE messages SET read_wrap = ?, manage_wrap = ?, record = ?, blob_ref = ?, blob_wrap = ?,
		blob_purge_at = ? WHERE locator = ?`
	updateRowAndPurge = `UPDATE messages SET read_wrap = ?, manage_wrap = ?, record = ?, blob_ref = ?, blob_wrap = ?,
		blob_purge_at = ?, purge_at = ? WHERE locator = ?`
)

// rowQuery fills each NULL with a value of the length a live row holds there.
var rowQuery = fmt.Sprintf(`SELECT locator, read_wrap, manage_wrap, record,
		ifnull(blob_ref, '%s'), blob_ref IS NULL,
		ifnull(blob_wrap, zeroblob(%d)), blob_wrap IS NULL,
		ifnull(blob_purge_at, 0), blob_purge_at IS NULL, purge_at
		FROM messages WHERE locator = ?`,
	strings.Repeat("0", base64.RawURLEncoding.EncodedLen(store.BlobReferenceLen)),
	seal.WrapLen,
)

// absentLocator is never stored: finding an id that hashes to it is a preimage.
var absentLocator seal.Locator = sha256.Sum256([]byte("nylzo/v1/absent-row"))

func (s *Store) Create(ctx context.Context, row *store.Row, counters ...store.RateHit) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		inserted, err := execIn(ctx, tx, s.statements.insertRow, rowValues(row)...)
		if err != nil {
			return err
		}
		if affected, countErr := inserted.RowsAffected(); countErr != nil || affected == 0 {
			return cmp.Or(countErr, store.ErrConflict)
		}
		statement := tx.StmtContext(ctx, s.statements.hitCounter)
		for i := range counters {
			if _, err = countHit(ctx, statement, &counters[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Load(ctx context.Context, locator seal.Locator) (store.Row, error) {
	return scanRow(s.statements.loadRow.QueryRowContext(ctx, locator[:]))
}

// Lookup makes one lookup that finds a row and one that finds none, whether or not the locator is known: an unknown
// locator gets the stored dummy row, a known one a miss on an absent locator (AK21, F1).
func (s *Store) Lookup(ctx context.Context, locator seal.Locator) (store.Row, bool, error) {
	row, err := s.Load(ctx, locator)
	switch {
	case err == nil:
		_, missErr := s.Load(ctx, absentLocator)
		if !errors.Is(missErr, store.ErrNotFound) {
			return store.Row{}, false, cmp.Or(missErr, store.ErrCorrupt)
		}
		return row, true, nil
	case errors.Is(err, store.ErrNotFound):
		dummy, dummyErr := s.Load(ctx, store.DummyLocator)
		return dummy, false, dummyErr
	default:
		return store.Row{}, false, err
	}
}

// ensureDummyRow stores the row that Lookup reads for an unknown locator; random keys seal it, so nothing opens it.
func ensureDummyRow(ctx context.Context, database *sql.DB) error {
	row := store.NewDummyRow()
	_, err := database.ExecContext(ctx, insertRow, rowValues(&row)...)
	return err
}

// Update hands the row to change inside one write transaction and stores what change replaced.
func (s *Store) Update(ctx context.Context, locator seal.Locator, change func(row *store.Row) error) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		reload := tx.StmtContext(ctx, s.statements.reloadRow)
		defer reload.Close()
		row, err := scanRow(reload.QueryRowContext(ctx, locator[:]))
		if err != nil {
			return err
		}
		stored := row
		if changeErr := change(&row); changeErr != nil {
			return changeErr
		}
		values := rowValues(&row)
		switch {
		case !stored.PurgeAt.Equal(row.PurgeAt):
			_, err = execIn(ctx, tx, s.statements.updateRowAndPurge, append(values[1:], locator[:])...)
		case !sameRow(&stored, &row):
			_, err = execIn(ctx, tx, s.statements.updateRow, slices.Concat(values[1:7], []any{locator[:]})...)
		default:
		}
		return err
	})
}

// sameRow ignores purge_at: SQLite rewrites its index whenever the column is set, even to the value it holds.
func sameRow(stored, changed *store.Row) bool {
	return stored.BlobReference == changed.BlobReference &&
		stored.BlobPurgeAt.Equal(changed.BlobPurgeAt) &&
		bytes.Equal(stored.ReadWrap, changed.ReadWrap) &&
		bytes.Equal(stored.ManageWrap, changed.ManageWrap) &&
		bytes.Equal(stored.Record, changed.Record) &&
		(stored.BlobWrap == nil) == (changed.BlobWrap == nil) &&
		bytes.Equal(stored.BlobWrap, changed.BlobWrap)
}

func rowValues(row *store.Row) []any {
	var blobReference, blobWrap, blobPurgeAt any
	if row.BlobReference != "" {
		blobReference = row.BlobReference
	}
	if row.BlobWrap != nil {
		blobWrap = row.BlobWrap
	}
	if !row.BlobPurgeAt.IsZero() {
		blobPurgeAt = milliseconds(row.BlobPurgeAt)
	}
	return []any{
		row.Locator[:],
		row.ReadWrap,
		row.ManageWrap,
		row.Record,
		blobReference,
		blobWrap,
		blobPurgeAt,
		milliseconds(row.PurgeAt),
	}
}

// scanRow reads every row in one shape, with fillers for its NULLs, so a live, an ended and the dummy row cost the
// same work (AK21, F1).
func scanRow(scanner *sql.Row) (store.Row, error) {
	var row store.Row
	var locator []byte
	var blobReference string
	var blobReferenceMissing, blobWrapMissing, blobPurgeMissing bool
	var blobPurgeAt, purgeAt int64
	err := scanner.Scan(
		&locator,
		&row.ReadWrap,
		&row.ManageWrap,
		&row.Record,
		&blobReference,
		&blobReferenceMissing,
		&row.BlobWrap,
		&blobWrapMissing,
		&blobPurgeAt,
		&blobPurgeMissing,
		&purgeAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Row{}, store.ErrNotFound
	}
	if err != nil {
		return store.Row{}, err
	}
	if len(locator) != seal.LocatorLen {
		return store.Row{}, store.ErrCorrupt
	}
	row.Locator = seal.Locator(locator)
	if !blobReferenceMissing {
		row.BlobReference = blobReference
	}
	if blobWrapMissing {
		row.BlobWrap = nil
	}
	if !blobPurgeMissing {
		row.BlobPurgeAt = fromMilliseconds(blobPurgeAt)
	}
	row.PurgeAt = fromMilliseconds(purgeAt)
	return row, nil
}

func milliseconds(moment time.Time) int64 {
	return moment.UnixMilli()
}

func fromMilliseconds(milliseconds int64) time.Time {
	return time.UnixMilli(milliseconds).UTC()
}
