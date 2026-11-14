package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type table struct {
	name       string
	definition string
	columns    string
	key        string
	index      string
}

var (
	messagesTable = table{
		name: "messages",
		definition: `CREATE TABLE IF NOT EXISTS messages (
	locator BLOB PRIMARY KEY,
	read_wrap BLOB NOT NULL,
	manage_wrap BLOB NOT NULL,
	record BLOB NOT NULL,
	blob_ref TEXT,
	blob_wrap BLOB,
	blob_purge_at INTEGER,
	purge_at INTEGER NOT NULL
) WITHOUT ROWID`,
		columns: "locator, read_wrap, manage_wrap, record, blob_ref, blob_wrap, blob_purge_at, purge_at",
		key:     "locator",
		index:   `CREATE INDEX IF NOT EXISTS messages_purge_at ON messages(purge_at)`,
	}
	countersTable = table{
		name: "rate_counters",
		definition: `CREATE TABLE IF NOT EXISTS rate_counters (
	key TEXT NOT NULL,
	window_start INTEGER NOT NULL,
	count INTEGER NOT NULL,
	bytes INTEGER NOT NULL,
	purge_at INTEGER NOT NULL,
	PRIMARY KEY (key, window_start)
) WITHOUT ROWID`,
		columns: "key, window_start, count, bytes, purge_at",
		key:     "key, window_start",
		index:   `CREATE INDEX IF NOT EXISTS rate_counters_purge_at ON rate_counters(purge_at)`,
	}
	rebuiltTables = []table{messagesTable, countersTable}
)

const scrubTable = `CREATE TABLE IF NOT EXISTS wal_scrub (
	slot INTEGER PRIMARY KEY,
	generation INTEGER NOT NULL,
	filler BLOB NOT NULL
)`

var schema = messagesTable.definition + ";\n" + messagesTable.index + ";\n" +
	countersTable.definition + ";\n" + countersTable.index + ";\n" +
	scrubTable + ";\n"

// schemaVersion 8 holds the 1024-byte records of protocol v3 with their OPAQUE registration.
const schemaVersion = 8

var (
	ErrNewerSchema    = errors.New("sqlite: the database was written by a newer nylzo")
	ErrReleasedSchema = errors.New(
		"sqlite: the database was written by a release of an earlier protocol version; nylzo serve upgrades it " +
			"once, which erases the messages stored in it",
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
	if versionErr := checkVersion(version); versionErr != nil {
		return versionErr
	}
	return prepareSchema(ctx, database, false)
}

func createSchema(ctx context.Context, database *sql.DB) error {
	return prepareSchema(ctx, database, true)
}

// prepareSchema reads the version in the write transaction that changes the schema, so concurrent opens change it
// once; a database without the counter purge index gains it and keeps every row.
func prepareSchema(ctx context.Context, database *sql.DB, create bool) error {
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
	var statements string
	switch {
	case create && version == 0 && objects == 0:
		statements = schema + fmt.Sprintf(`PRAGMA user_version = %d;`, schemaVersion)
	case version == schemaVersion:
		statements = countersTable.index
	default:
		return checkVersion(version)
	}
	if _, err = tx.ExecContext(ctx, statements); err != nil {
		return err
	}
	return tx.Commit()
}

func checkVersion(version int) error {
	switch {
	case version > schemaVersion:
		return fmt.Errorf("%w: version %d, this build knows %d", ErrNewerSchema, version, schemaVersion)
	case isReleased(version):
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
