package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
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
`

// unsealedTables held messages in the clear before version 5; without their keys they cannot be sealed.
var unsealedTables = []string{
	`DROP TABLE IF EXISTS leases`,
	`DROP TABLE IF EXISTS tombstones`,
	`DROP TABLE IF EXISTS passphrase_attempts`,
}

// unusedIndexes cost a page on every write: the sweep pages blobs by locator, and rate_counters stays small.
var unusedIndexes = []string{
	`DROP INDEX IF EXISTS messages_blob_purge_at`,
	`DROP INDEX IF EXISTS rate_counters_purge_at`,
}

const olderBuildWrite = `SELECT RAISE(ABORT, 'nylzo: this write comes from an older nylzod than the database schema allows')`

var guards = fmt.Sprintf(`
CREATE TRIGGER IF NOT EXISTS rate_counters_guard_insert BEFORE INSERT ON rate_counters WHEN %[1]s BEGIN %[2]s; END;
CREATE TRIGGER IF NOT EXISTS rate_counters_guard_update BEFORE UPDATE ON rate_counters WHEN %[1]s BEGIN %[2]s; END;
`, revealingCounter("NEW."), olderBuildWrite)

func revealingCounter(prefix string) string {
	return fmt.Sprintf(
		"%[1]skey GLOB 'create:*' OR %[1]skey GLOB 'read:*' OR %[1]skey GLOB 'abuse:h:*' OR %[1]skey GLOB 'abuse:id:*:*' OR (%[1]skey GLOB 'pow:*' AND %[1]swindow_start != 0)",
		prefix,
	)
}

const schemaVersion = 5

var (
	ErrNewerSchema = errors.New("sqlite: the database was written by a newer nylzo")
	ErrOlderSchema = errors.New("sqlite: the database has an older schema; start nylzo serve to migrate it")
)

var deleteRevealingCounters = `DELETE FROM rate_counters WHERE key GLOB 'abuse:id:*' OR key GLOB 'sweep:reconcile:*' OR ` + revealingCounter(
	"",
)

type unorderedTable struct {
	name, columns string
}

var countersTable = unorderedTable{
	name:    "rate_counters",
	columns: "key, window_start, count, bytes, purge_at",
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row
}

type executor interface {
	ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error)
}

func requireCurrent(ctx context.Context, database *sql.DB) error {
	version, err := userVersion(ctx, database)
	switch {
	case err != nil:
		return err
	case version > schemaVersion:
		return newerSchema(version)
	case version < schemaVersion:
		return fmt.Errorf("%w: version %d, this build needs %d", ErrOlderSchema, version, schemaVersion)
	default:
		return nil
	}
}

func migrate(ctx context.Context, database *sql.DB) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	version, err := userVersion(ctx, connection)
	if err != nil {
		return err
	}
	if version > schemaVersion {
		return newerSchema(version)
	}
	err = execAll(ctx, connection, `PRAGMA foreign_keys = OFF`)
	if err == nil {
		err = migrateTables(ctx, connection, version)
	}
	if err == nil {
		err = execAll(ctx, connection, `PRAGMA foreign_keys = ON`)
	}
	if err != nil || version >= schemaVersion {
		return err
	}
	return execAll(ctx, connection, `VACUUM`, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
}

// migrateTables drops a messages table only while it still has the unsealed id column, so a repeat is harmless.
func migrateTables(ctx context.Context, connection *sql.Conn, version int) error {
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	unsealed, err := hasColumn(ctx, tx, "messages", "id")
	if err != nil {
		return err
	}
	statements := slices.Clone(unsealedTables)
	if unsealed {
		statements = append(statements, `DROP TABLE messages`)
	}
	err = execAll(ctx, tx, append(append(statements, schema), unusedIndexes...)...)
	if err == nil {
		err = dropRowid(ctx, tx, countersTable)
	}
	if err == nil && version < schemaVersion {
		err = execAll(ctx, tx, deleteRevealingCounters)
	}
	if err == nil {
		err = execAll(ctx, tx, guards)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func hasColumn(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).
		Scan(&found)
	return found > 0, err
}

func dropRowid(ctx context.Context, tx *sql.Tx, table unorderedTable) error {
	var definition string
	err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table.name).
		Scan(&definition)
	if err != nil || strings.HasSuffix(strings.TrimSpace(definition), "WITHOUT ROWID") {
		return err
	}
	rebuilt := table.name + "_rebuilt"
	return execAll(ctx, tx,
		strings.Replace(definition, table.name, rebuilt, 1)+" WITHOUT ROWID",
		fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s`, rebuilt, table.columns, table.columns, table.name),
		`DROP TABLE `+table.name,
		fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, rebuilt, table.name),
	)
}

func userVersion(ctx context.Context, database queryRower) (int, error) {
	var version int
	err := database.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
	return version, err
}

func newerSchema(version int) error {
	return fmt.Errorf("%w: version %d, this build knows %d", ErrNewerSchema, version, schemaVersion)
}

func execAll(ctx context.Context, target executor, statements ...string) error {
	for _, statement := range statements {
		if _, err := target.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
