package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

const resealUpdate = `UPDATE messages SET read_wrap = ?, manage_wrap = ?
	WHERE locator = ? AND read_wrap = ? AND manage_wrap = ?`

// resealQuery skips a locator of another length, as the sweep does, so such a row stops no start.
var resealQuery = fmt.Sprintf(
	`SELECT locator, read_wrap, manage_wrap FROM messages WHERE locator > ? AND length(locator) = %d
		ORDER BY locator LIMIT ?`,
	seal.LocatorLen,
)

// ResealWraps reads the rows in batches of store.SweepBatch and writes a batch back in one write transaction only when
// a wrap in it changed. It then rebuilds the tables and scrubs the whole log, so neither free space nor a frame keeps a
// wrap it replaced or one that an earlier process left behind (spec §5.2).
func (s *Store) ResealWraps(ctx context.Context, reseal func(row *store.Row) bool) error {
	after := []byte{}
	for {
		batch, err := wrapsAfter(ctx, s.reader, after)
		if err == nil {
			err = s.storeResealed(ctx, batch, reseal)
		}
		if err != nil {
			return err
		}
		if len(batch) < store.SweepBatch {
			break
		}
		last := batch[len(batch)-1].Locator
		after = last[:]
	}
	s.changedSinceBuild.Store(true)
	return s.checkpointWithRetries(ctx)
}

type resealedRow struct {
	row                  *store.Row
	readWrap, manageWrap []byte
}

// storeResealed writes a wrap only over the value it read, so a row that changed meanwhile keeps its own.
func (s *Store) storeResealed(ctx context.Context, batch []store.Row, reseal func(row *store.Row) bool) error {
	var changed []resealedRow
	for i := range batch {
		read, manage := batch[i].ReadWrap, batch[i].ManageWrap
		if reseal(&batch[i]) {
			changed = append(changed, resealedRow{row: &batch[i], readWrap: read, manageWrap: manage})
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		for _, resealed := range changed {
			row := resealed.row
			if _, err := tx.ExecContext(
				ctx,
				resealUpdate,
				row.ReadWrap,
				row.ManageWrap,
				row.Locator[:],
				resealed.readWrap,
				resealed.manageWrap,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func wrapsAfter(ctx context.Context, database *sql.DB, after []byte) ([]store.Row, error) {
	rows, err := database.QueryContext(ctx, resealQuery, after, store.SweepBatch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	batch := make([]store.Row, 0, store.SweepBatch)
	for rows.Next() {
		var row store.Row
		var locator []byte
		if scanErr := rows.Scan(&locator, &row.ReadWrap, &row.ManageWrap); scanErr != nil {
			return nil, scanErr
		}
		if len(locator) != seal.LocatorLen {
			return nil, store.ErrCorrupt
		}
		row.Locator = seal.Locator(locator)
		batch = append(batch, row)
	}
	return batch, rows.Err()
}
