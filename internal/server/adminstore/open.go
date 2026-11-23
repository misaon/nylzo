// Package adminstore keeps what the operator's admin area needs between restarts: its passkeys, the one enrollment
// that is open, and the counts behind its charts. It holds no address and nothing about a single message.
package adminstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"

	_ "modernc.org/sqlite" // registers the sqlite driver
)

const (
	FileName      = "admin.db"
	journalSuffix = "-journal"
	schemaVersion = 1
	privateMode   = 0o600
	driverName    = "sqlite"
)

const schema = `CREATE TABLE passkeys (
	credential_id BLOB PRIMARY KEY,
	public_key BLOB NOT NULL,
	algorithm INTEGER NOT NULL,
	sign_count INTEGER NOT NULL,
	backup_eligible INTEGER NOT NULL,
	backed_up INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	last_used_at INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE enrollment (
	slot INTEGER PRIMARY KEY CHECK (slot = 1),
	token_hash BLOB NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE TABLE counts (
	resolution INTEGER NOT NULL,
	period_start INTEGER NOT NULL,
	metric TEXT NOT NULL,
	value INTEGER NOT NULL,
	PRIMARY KEY (resolution, period_start, metric)
) WITHOUT ROWID;
CREATE TABLE carried (
	period_start INTEGER NOT NULL,
	metric TEXT NOT NULL,
	value INTEGER NOT NULL,
	PRIMARY KEY (period_start, metric)
) WITHOUT ROWID;
`

var errNewerSchema = errors.New("adminstore: the admin database was written by a newer nylzo")

// ErrJournalNotErased reports a change that is stored although its journal could not be overwritten; the next
// write overwrites it.
var ErrJournalNotErased = errors.New("adminstore: the journal was not overwritten")

type Store struct {
	database *sql.DB
	journal  string
}

// Files lists the database and its rollback journal. SQLite keeps the journal file between writes, so the pages
// it copied there stay inside the file, where the store overwrites them, and never reach free disk space.
func Files(path string) []string {
	return []string{path, path + journalSuffix}
}

func Open(ctx context.Context, path string) (*Store, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, privateMode)
	if err == nil {
		err = file.Close()
	}
	if err != nil {
		return nil, err
	}
	// sql.Open fails only for an unregistered driver.
	database, _ := sql.Open(driverName, dataSourceName(path))
	database.SetMaxOpenConns(1)
	store := &Store{database: database, journal: path + journalSuffix}
	if err = store.prepare(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("adminstore: %w", err), database.Close())
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.database.Close()
}

// prepare also reads the passkeys, so a file whose tables are gone or damaged fails here, where the server can
// still start without its admin area, and not at its first request.
func (s *Store) prepare(ctx context.Context) error {
	if err := prepareSchema(ctx, s.database); err != nil {
		return err
	}
	if _, err := s.CountPasskeys(ctx); err != nil {
		return err
	}
	return s.eraseJournal(ctx)
}

func dataSourceName(path string) string {
	query := url.Values{}
	query.Set("_txlock", "immediate")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(PERSIST)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Add("_pragma", "secure_delete(1)")
	query.Add("_pragma", "temp_store(2)")
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + query.Encode()
}

func prepareSchema(ctx context.Context, database *sql.DB) error {
	return inTransaction(ctx, database, func(tx *sql.Tx) error {
		var version int
		err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
		if err != nil || version == schemaVersion {
			return err
		}
		if version > schemaVersion {
			return fmt.Errorf("%w: version %d, this build knows %d", errNewerSchema, version, schemaVersion)
		}
		_, err = tx.ExecContext(ctx, schema+fmt.Sprintf(`PRAGMA user_version = %d;`, schemaVersion))
		return err
	})
}

func inTransaction(ctx context.Context, database *sql.DB, body func(tx *sql.Tx) error) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if bodyErr := body(tx); bodyErr != nil {
		return bodyErr
	}
	return tx.Commit()
}

// write runs a change and then overwrites the journal, which holds the pages as they were before it: a removed
// passkey, a used token, a count before it grew.
func (s *Store) write(ctx context.Context, body func(tx *sql.Tx) error) error {
	if err := inTransaction(ctx, s.database, body); err != nil {
		return err
	}
	// The change is stored, so a request that ends now must not leave its journal behind.
	if err := s.eraseJournal(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("%w: %w", ErrJournalNotErased, err)
	}
	return nil
}

// eraseJournal holds the write lock without writing, so no other writer has the journal in use while it turns to
// zeros.
func (s *Store) eraseJournal(ctx context.Context) error {
	return inTransaction(ctx, s.database, func(*sql.Tx) error {
		journal, err := os.OpenFile(s.journal, os.O_WRONLY, 0)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		info, err := journal.Stat()
		if err == nil {
			_, err = journal.Write(make([]byte, info.Size()))
		}
		if err == nil {
			err = journal.Sync()
		}
		return errors.Join(err, journal.Close())
	})
}
