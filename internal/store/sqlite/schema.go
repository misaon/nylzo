package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const schema = `
CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	version INTEGER NOT NULL,
	kdf_id INTEGER NOT NULL,
	flags INTEGER NOT NULL,
	state TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	ttl_s INTEGER NOT NULL,
	opens_max INTEGER NOT NULL,
	opens_remaining INTEGER NOT NULL,
	pepper_id INTEGER NOT NULL,
	read_mac BLOB NOT NULL,
	manage_mac BLOB NOT NULL,
	verifier_mac BLOB,
	size INTEGER NOT NULL,
	blob_ref TEXT,
	fail_count_total INTEGER NOT NULL DEFAULT 0,
	next_attempt_at INTEGER NOT NULL DEFAULT 0,
	first_opened_at INTEGER,
	ending TEXT
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS messages_expires_at ON messages(expires_at);
CREATE TABLE IF NOT EXISTS leases (
	row_id INTEGER PRIMARY KEY,
	id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	lease_mac BLOB NOT NULL,
	pepper_id INTEGER NOT NULL,
	lease_expires_at INTEGER NOT NULL,
	downloads INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS leases_id ON leases(id);
CREATE INDEX IF NOT EXISTS leases_expires_at ON leases(lease_expires_at);
CREATE TABLE IF NOT EXISTS tombstones (
	id TEXT PRIMARY KEY,
	pepper_id INTEGER NOT NULL,
	read_mac BLOB NOT NULL,
	manage_mac BLOB NOT NULL,
	state TEXT NOT NULL,
	first_opened_at INTEGER,
	ended_at INTEGER NOT NULL,
	purge_at INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS tombstones_purge_at ON tombstones(purge_at);
CREATE TABLE IF NOT EXISTS rate_counters (
	key TEXT NOT NULL,
	window_start INTEGER NOT NULL,
	count INTEGER NOT NULL,
	bytes INTEGER NOT NULL,
	purge_at INTEGER NOT NULL,
	PRIMARY KEY (key, window_start)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS rate_counters_purge_at ON rate_counters(purge_at);
`

const indexes = `
DROP INDEX IF EXISTS messages_release;
DROP INDEX IF EXISTS messages_release_id;
CREATE INDEX IF NOT EXISTS messages_releasable ON messages(id) WHERE opens_remaining = 0 AND blob_ref IS NOT NULL;
CREATE INDEX IF NOT EXISTS messages_ending ON messages(id) WHERE ending IS NOT NULL;
`

const olderBuildWrite = `SELECT RAISE(ABORT, 'nylzo: this write comes from an older nylzod than the database schema allows')`

var guards = fmt.Sprintf(`
CREATE TRIGGER IF NOT EXISTS rate_counters_guard_insert BEFORE INSERT ON rate_counters WHEN %[1]s BEGIN %[3]s; END;
CREATE TRIGGER IF NOT EXISTS rate_counters_guard_update BEFORE UPDATE ON rate_counters WHEN %[1]s BEGIN %[3]s; END;
CREATE TRIGGER IF NOT EXISTS leases_guard_insert BEFORE INSERT ON leases WHEN %[2]s BEGIN %[3]s; END;
CREATE TRIGGER IF NOT EXISTS leases_guard_update BEFORE UPDATE OF row_id ON leases WHEN %[2]s BEGIN %[3]s; END;
`, revealingCounter("NEW."), fmt.Sprintf("NEW.row_id IS NULL OR NEW.row_id < %d", minLeaseRowID), olderBuildWrite)

func revealingCounter(prefix string) string {
	return fmt.Sprintf(
		"%[1]skey GLOB 'create:*' OR %[1]skey GLOB 'read:*' OR %[1]skey GLOB 'abuse:h:*' OR %[1]skey GLOB 'abuse:id:*:*' OR (%[1]skey GLOB 'pow:*' AND %[1]swindow_start != 0)",
		prefix,
	)
}

const (
	schemaVersion = 4
	minLeaseRowID = 1 << 32
)

var (
	ErrNewerSchema = errors.New("sqlite: the database was written by a newer nylzo")
	ErrOlderSchema = errors.New("sqlite: the database has an older schema; start nylzo serve to migrate it")
)

var deleteRevealingCounters = `DELETE FROM rate_counters WHERE ` + revealingCounter("")

type unorderedTable struct {
	name, columns, index string
}

var unorderedTables = []unorderedTable{
	{
		name:    "messages",
		columns: "id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, verifier_mac, size, blob_ref, fail_count_total, next_attempt_at, first_opened_at, ending",
		index:   "expires_at",
	},
	{
		name:    "tombstones",
		columns: "id, pepper_id, read_mac, manage_mac, state, first_opened_at, ended_at, purge_at",
		index:   "purge_at",
	},
	{name: "rate_counters", columns: "key, window_start, count, bytes, purge_at", index: "purge_at"},
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
	err = execAll(ctx, connection, schema, `PRAGMA foreign_keys = OFF`)
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

func migrateTables(ctx context.Context, connection *sql.Conn, version int) error {
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = upgradeColumns(ctx, tx)
	for _, table := range unorderedTables {
		if err == nil {
			err = dropRowid(ctx, tx, table)
		}
	}
	if err == nil && version < schemaVersion {
		err = execAll(ctx, tx, deleteRevealingCounters)
	}
	if err == nil {
		err = execAll(ctx, tx, indexes+guards)
	}
	if err != nil {
		return err
	}
	var violations int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations)
	if err != nil {
		return err
	}
	if violations != 0 {
		return fmt.Errorf("sqlite: migration left %d foreign key violations", violations)
	}
	return tx.Commit()
}

func upgradeColumns(ctx context.Context, tx *sql.Tx) error {
	columns, err := messageColumns(ctx, tx)
	if err != nil {
		return err
	}
	var statements []string
	if !columns["ending"] {
		statements = append(statements, `ALTER TABLE messages ADD COLUMN ending TEXT`)
	}
	if !columns["next_attempt_at"] {
		statements = append(statements, `ALTER TABLE messages ADD COLUMN next_attempt_at INTEGER NOT NULL DEFAULT 0`)
	}
	for _, perAddress := range []string{"day_fail_count", "day_fail_date"} {
		if columns[perAddress] {
			statements = append(statements, `ALTER TABLE messages DROP COLUMN `+perAddress)
		}
	}
	statements = append(statements, `DROP TABLE IF EXISTS passphrase_attempts`)
	return execAll(ctx, tx, statements...)
}

func messageColumns(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info('messages')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, scanErr
		}
		columns[name] = true
	}
	return columns, rows.Err()
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
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_%s ON %s(%s)`, table.name, table.index, table.name, table.index),
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
