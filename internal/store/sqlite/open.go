package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	driver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/misaon/nylzo/internal/store"
)

const (
	readerConnections       = 8
	openRetryFor            = 10 * time.Second
	openBackoff             = 20 * time.Millisecond
	openMaxWait             = 200 * time.Millisecond
	maxCacheKiB             = 512
	busyTimeoutMilliseconds = 5000
)

var errNoFileControl = errors.New("sqlite: the driver cannot keep the log file, so it would free its blocks")

type Store struct {
	path        string
	pageSize    int
	writer      *sql.DB
	reader      *sql.DB
	statements  *statements
	scrubs      chan struct{}
	stopScrubs  context.CancelFunc
	scrubbersUp sync.WaitGroup
}

var _ store.Store = (*Store)(nil)

func Open(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, createSchema)
}

func OpenCurrent(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, requireCurrent)
}

// Close scrubs the log once more, so a process that ended messages leaves no older row version in it.
func (s *Store) Close() error {
	s.stopScrubs()
	s.scrubbersUp.Wait()
	scrubErr := s.Checkpoint(context.Background())
	if errors.Is(scrubErr, ErrCheckpointBusy) {
		scrubErr = nil
	}
	return errors.Join(scrubErr, s.statements.close(), s.reader.Close(), s.writer.Close())
}

func (s *Store) transaction(ctx context.Context, body func(tx *sql.Tx) error) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = body(tx)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return cmp.Or(ctx.Err(), err)
	}
	return nil
}

type statements struct {
	loadRow        *sql.Stmt
	hitCounter     *sql.Stmt
	reserveCounter *sql.Stmt
}

func prepare(ctx context.Context, writer, reader *sql.DB) (*statements, error) {
	prepared := &statements{}
	var errs [3]error
	prepared.loadRow, errs[0] = reader.PrepareContext(ctx, rowQuery)
	prepared.hitCounter, errs[1] = writer.PrepareContext(ctx, hitQuery)
	prepared.reserveCounter, errs[2] = writer.PrepareContext(ctx, reserveQuery)
	if err := errors.Join(errs[:]...); err != nil {
		return nil, errors.Join(err, prepared.close())
	}
	return prepared, nil
}

func (p *statements) close() error {
	var errs []error
	for _, statement := range []*sql.Stmt{p.loadRow, p.hitCounter, p.reserveCounter} {
		if statement != nil {
			errs = append(errs, statement.Close())
		}
	}
	return errors.Join(errs...)
}

func openWith(ctx context.Context, path string, prepareSchema func(context.Context, *sql.DB) error) (*Store, error) {
	if err := createPrivate(ctx, path); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(openRetryFor)
	for attempt := 1; ; attempt++ {
		opened, err := connect(ctx, path, prepareSchema)
		if err == nil || !busy(err) || time.Now().After(deadline) {
			return opened, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryDelay(attempt)):
		}
	}
}

func createPrivate(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	return file.Close()
}

func retryDelay(attempt int) time.Duration {
	//nolint:gosec // G404: the jitter between open retries needs no unpredictability.
	jitter := rand.N(openBackoff)
	return min(time.Duration(attempt)*openBackoff, openMaxWait) + jitter
}

func busy(err error) bool {
	sqliteErr, ok := errors.AsType[*driver.Error](err)
	return ok && sqliteErr.Code()&0xff == sqlite3.SQLITE_BUSY
}

func connect(ctx context.Context, path string, prepareSchema func(context.Context, *sql.DB) error) (*Store, error) {
	writer, err := openPersistent(path, true)
	if err != nil {
		return nil, err
	}
	writer.SetMaxOpenConns(1)
	if err = prepareSchema(ctx, writer); err != nil {
		writer.Close()
		return nil, fmt.Errorf("sqlite: schema: %w", err)
	}
	reader, err := openPersistent(path, false)
	if err != nil {
		writer.Close()
		return nil, err
	}
	reader.SetMaxOpenConns(readerConnections)
	reader.SetMaxIdleConns(readerConnections)
	opened := &Store{path: path, writer: writer, reader: reader, scrubs: make(chan struct{}, 1)}
	err = reader.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&opened.pageSize)
	if err == nil {
		opened.statements, err = prepare(ctx, writer, reader)
	}
	if err != nil {
		reader.Close()
		writer.Close()
		return nil, err
	}
	scrubContext, stop := context.WithCancel(context.WithoutCancel(ctx))
	opened.stopScrubs = stop
	opened.scrubbersUp.Go(func() { opened.scrubWhenAsked(scrubContext) })
	return opened, nil
}

// openPersistent keeps the log file when a connection closes, so its blocks are overwritten and never freed.
func openPersistent(path string, write bool) (*sql.DB, error) {
	connector, err := driver.NewConnector(dataSourceName(path, write))
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(persistentLog{Connector: connector}), nil
}

type persistentLog struct {
	sqldriver.Connector
}

func (p persistentLog) Connect(ctx context.Context) (sqldriver.Conn, error) {
	connection, err := p.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	control, ok := connection.(driver.FileControl)
	if !ok {
		connection.Close()
		return nil, errNoFileControl
	}
	if _, err = control.FileControlPersistWAL("main", 1); err != nil {
		connection.Close()
		return nil, err
	}
	return connection, nil
}

func dataSourceName(path string, write bool) string {
	query := url.Values{}
	if write {
		query.Set("_txlock", "immediate")
	}
	query.Set("_busy_timeout", strconv.Itoa(busyTimeoutMilliseconds))
	query.Set("_journal_mode", "WAL")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "synchronous(FULL)")
	query.Add("_pragma", "secure_delete(1)")
	query.Add("_pragma", "journal_size_limit(-1)")
	query.Add("_pragma", "wal_autocheckpoint(1)")
	query.Add("_pragma", fmt.Sprintf("cache_size(-%d)", maxCacheKiB))
	query.Add("_pragma", "temp_store(2)")
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + query.Encode()
}
