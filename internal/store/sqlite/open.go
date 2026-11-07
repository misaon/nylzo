package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
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

var (
	errNoFileControl = errors.New("sqlite: the driver cannot keep the log file, so it would free its blocks")
	errNoStatus      = errors.New("sqlite: the driver cannot count the pages a write puts into the log")
)

type Store struct {
	path             string
	pageSize         int
	writer           *sql.DB
	reader           *sql.DB
	statements       *statements
	scrubs           chan struct{}
	stopScrubs       context.CancelFunc
	scrubbersUp      sync.WaitGroup
	scrubFailed      atomic.Pointer[func(error)]
	unscrubbedFrames atomic.Int64
}

var _ store.Store = (*Store)(nil)

func Open(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, func(ctx context.Context, database *sql.DB) error {
		if err := createSchema(ctx, database); err != nil {
			return err
		}
		return ensureDummyRow(ctx, database)
	})
}

func OpenCurrent(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, requireCurrent)
}

// Close scrubs the log once more, so a process that changed rows leaves no older row version in it.
func (s *Store) Close() error {
	s.stopScrubs()
	s.scrubbersUp.Wait()
	scrubErr := s.Checkpoint(context.Background())
	if errors.Is(scrubErr, ErrCheckpointBusy) {
		scrubErr = nil
	}
	return errors.Join(scrubErr, s.statements.close(), s.reader.Close(), s.writer.Close())
}

// transaction commits body on the only writer connection, then asks for a scrub of the frames the commit wrote.
func (s *Store) transaction(ctx context.Context, body func(tx *sql.Tx) error) error {
	connection, err := s.writer.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	if _, err = writtenPages(connection); err != nil {
		return err
	}
	err = commit(ctx, connection, body)
	s.noteFrames(ctx, connection, err == nil)
	return err
}

func commit(ctx context.Context, connection *sql.Conn, body func(tx *sql.Tx) error) error {
	tx, err := connection.BeginTx(ctx, nil)
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

// noteFrames asks for a scrub of the frames a transaction wrote; it asks for a full scrub when it cannot tell the last
// frame, or when a transaction that rolled back had spilled pages into the log.
func (s *Store) noteFrames(ctx context.Context, connection *sql.Conn, committed bool) {
	pages, err := writtenPages(connection)
	if err == nil && pages == 0 {
		return
	}
	frames := int64(math.MaxInt64)
	if err == nil && committed {
		if logged, logErr := logFrames(context.WithoutCancel(ctx), connection); logErr == nil {
			frames = logged
		}
	}
	if frames > s.unscrubbedFrames.Load() {
		s.unscrubbedFrames.Store(frames)
	}
	s.requestScrub()
}

// writtenPages counts the pages the connection wrote to the log since it last asked, spilled pages included.
func writtenPages(connection *sql.Conn) (int, error) {
	var pages int
	err := connection.Raw(func(driverConnection any) error {
		status, ok := driverConnection.(driver.DBStatus)
		if !ok {
			return errNoStatus
		}
		var statusErr error
		pages, _, statusErr = status.Status(driver.DBStatusCacheWrite, true)
		return statusErr
	})
	return pages, err
}

// logFrames reads the last frame of the log that holds a commit since the log last started over.
func logFrames(ctx context.Context, connection *sql.Conn) (int64, error) {
	var blocked, frames, moved int64
	err := connection.QueryRowContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`).Scan(&blocked, &frames, &moved)
	return frames, err
}

type statements struct {
	loadRow           *sql.Stmt
	reloadRow         *sql.Stmt
	insertRow         *sql.Stmt
	updateRow         *sql.Stmt
	updateRowAndPurge *sql.Stmt
	shredBlob         *sql.Stmt
	forgetBlob        *sql.Stmt
	hitCounter        *sql.Stmt
	reserveCounter    *sql.Stmt
	releaseCounter    *sql.Stmt
	loadCounter       *sql.Stmt
}

func prepare(ctx context.Context, writer, reader *sql.DB) (*statements, error) {
	prepared := &statements{}
	var errs [11]error
	prepared.loadRow, errs[0] = reader.PrepareContext(ctx, rowQuery)
	prepared.reloadRow, errs[1] = writer.PrepareContext(ctx, rowQuery)
	prepared.insertRow, errs[2] = writer.PrepareContext(ctx, insertRow)
	prepared.updateRow, errs[3] = writer.PrepareContext(ctx, updateRow)
	prepared.updateRowAndPurge, errs[4] = writer.PrepareContext(ctx, updateRowAndPurge)
	prepared.shredBlob, errs[5] = writer.PrepareContext(ctx, shredBlob)
	prepared.forgetBlob, errs[6] = writer.PrepareContext(ctx, forgetBlob)
	prepared.hitCounter, errs[7] = writer.PrepareContext(ctx, hitQuery)
	prepared.reserveCounter, errs[8] = writer.PrepareContext(ctx, reserveQuery)
	prepared.releaseCounter, errs[9] = writer.PrepareContext(ctx, releaseQuery)
	prepared.loadCounter, errs[10] = reader.PrepareContext(ctx, counterQuery)
	if err := errors.Join(errs[:]...); err != nil {
		return nil, errors.Join(err, prepared.close())
	}
	return prepared, nil
}

func (p *statements) close() error {
	all := []*sql.Stmt{
		p.loadRow,
		p.reloadRow,
		p.insertRow,
		p.updateRow,
		p.updateRowAndPurge,
		p.shredBlob,
		p.forgetBlob,
		p.hitCounter,
		p.reserveCounter,
		p.releaseCounter,
		p.loadCounter,
	}
	errs := make([]error, 0, len(all))
	for _, statement := range all {
		if statement != nil {
			errs = append(errs, statement.Close())
		}
	}
	return errors.Join(errs...)
}

// execIn runs a prepared statement inside tx; closing it leaves the statement prepared on the connection.
func execIn(ctx context.Context, tx *sql.Tx, prepared *sql.Stmt, arguments ...any) (sql.Result, error) {
	statement := tx.StmtContext(ctx, prepared)
	defer statement.Close()
	return statement.ExecContext(ctx, arguments...)
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
