package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const schema = `
CREATE TABLE IF NOT EXISTS messages (
	locator BLOB PRIMARY KEY,
	read_wrap BLOB NOT NULL,
	manage_wrap BLOB NOT NULL,
	record BLOB NOT NULL,
	blob_ref TEXT,
	blob_wrap BLOB,
	blob_purge_at INTEGER,
	purge_at INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS messages_purge_at ON messages(purge_at);
CREATE TABLE IF NOT EXISTS rate_counters (
	key TEXT NOT NULL,
	window_start INTEGER NOT NULL,
	count INTEGER NOT NULL,
	bytes INTEGER NOT NULL,
	purge_at INTEGER NOT NULL,
	PRIMARY KEY (key, window_start)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS wal_scrub (
	slot INTEGER PRIMARY KEY,
	generation INTEGER NOT NULL,
	filler BLOB NOT NULL
);
`

const schemaVersion = 7

var (
	ErrNewerSchema    = errors.New("sqlite: the database was written by a newer nylzo")
	ErrReleasedSchema = errors.New(
		"sqlite: the database was written by nylzo 0.1 or 0.2; nylzo serve upgrades it once, " +
			"which erases the messages stored in it",
	)
	ErrOlderSchema = errors.New(
		"sqlite: the database comes from a development build that no release used; " +
			"nylzo does not convert it, so start with an empty data directory",
	)
)

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row
}

func requireCurrent(ctx context.Context, database *sql.DB) error {
	version, err := userVersion(ctx, database)
	if err != nil {
		return err
	}
	return checkVersion(version)
}

// createSchema reads the version in the write transaction that creates the schema, so concurrent opens create it once.
func createSchema(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	version, err := userVersion(ctx, tx)
	if err != nil {
		return err
	}
	var objects int
	if countErr := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master`).Scan(&objects); countErr != nil {
		return countErr
	}
	if version != 0 || objects > 0 {
		return checkVersion(version)
	}
	if _, err = tx.ExecContext(ctx, schema+fmt.Sprintf(`PRAGMA user_version = %d;`, schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func checkVersion(version int) error {
	switch {
	case version > schemaVersion:
		return fmt.Errorf("%w: version %d, this build knows %d", ErrNewerSchema, version, schemaVersion)
	case version == releasedSchema:
		return fmt.Errorf("%w: version %d, this build reads only %d", ErrReleasedSchema, version, schemaVersion)
	case version < schemaVersion:
		return fmt.Errorf("%w: version %d, this build reads only %d", ErrOlderSchema, version, schemaVersion)
	default:
		return nil
	}
}

func userVersion(ctx context.Context, database queryRower) (int, error) {
	var version int
	err := database.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
	return version, err
}
