package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// rebuildChanged writes messages and rate_counters into fresh b-trees once a row changed here or in another process
// since the last rebuild: a page split leaves copies of the cells it moved in the free space of the page, where
// secure_delete never reaches (spec §5.2).
func (s *Store) rebuildChanged(ctx context.Context) error {
	connection, err := s.writer.Conn(ctx)
	if err != nil {
		return wrapRebuild(err)
	}
	defer connection.Close()
	version, err := dataVersion(ctx, connection)
	if err != nil {
		return wrapRebuild(err)
	}
	if !s.changedSinceBuild.Swap(false) && version == s.builtAtDataVersion.Load() {
		return nil
	}
	if _, err = writtenPages(connection); err == nil {
		err = commit(ctx, connection, func(tx *sql.Tx) error { return rebuildTables(ctx, tx) })
	}
	s.noteFrames(ctx, connection, err == nil)
	if err != nil {
		s.changedSinceBuild.Store(true)
		return wrapRebuild(err)
	}
	s.builtAtDataVersion.Store(version)
	return nil
}

// rebuildTables copies each table into memory, drops it, which zeroes every page it held, and inserts the rows again
// in key order into the pages it freed.
func rebuildTables(ctx context.Context, tx *sql.Tx) error {
	for _, rebuilt := range rebuiltTables {
		for _, statement := range []string{
			`DROP TABLE IF EXISTS temp.rebuilt_rows`,
			`CREATE TEMP TABLE rebuilt_rows AS SELECT ` + rebuilt.columns + ` FROM main.` + rebuilt.name,
			`DROP TABLE main.` + rebuilt.name,
			rebuilt.definition,
			`INSERT INTO main.` + rebuilt.name + ` (` + rebuilt.columns + `) SELECT ` + rebuilt.columns +
				` FROM temp.rebuilt_rows ORDER BY ` + rebuilt.key,
			rebuilt.index,
			`DROP TABLE temp.rebuilt_rows`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	return nil
}

// dataVersion changes when another connection commits, whatever process it belongs to.
func dataVersion(ctx context.Context, connection *sql.Conn) (int64, error) {
	var version int64
	err := connection.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&version)
	return version, err
}

func wrapRebuild(err error) error {
	if busy(err) {
		return fmt.Errorf("%w (%v)", ErrCheckpointBusy, err)
	}
	return fmt.Errorf("sqlite: rebuild: %w", err)
}
