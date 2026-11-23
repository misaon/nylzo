package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
)

const insertCounter = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES ('k', 0, 1, 0, 0)`

func openTest(t *testing.T) *Store {
	t.Helper()
	opened, err := Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return opened
}

func TestConnectionsKeepTheirCacheAndTemporaryCopiesInMemory(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	for _, database := range []*sql.DB{storage.writer, storage.reader} {
		var cacheKiB, temporaryStore int
		if err := database.QueryRow("PRAGMA cache_size").Scan(&cacheKiB); err != nil {
			t.Fatal(err)
		}
		if cacheKiB >= 0 || -cacheKiB > maxCacheKiB {
			t.Fatalf("page cache %d, want at most %d KiB per connection outside the Go heap", cacheKiB, maxCacheKiB)
		}
		if err := database.QueryRow("PRAGMA temp_store").Scan(&temporaryStore); err != nil || temporaryStore != 2 {
			t.Fatalf(
				"temp_store %d %v: SQLite may write temporary copies of rows outside the data directory",
				temporaryStore,
				err,
			)
		}
	}
}

func TestSchemaCreationStopsWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", dataSourceName(filepath.Join(t.TempDir(), "nylzo.db"), true))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = createSchema(ctx, database); !errors.Is(err, context.Canceled) {
		t.Fatalf("schema creation with a cancelled context: %v", err)
	}
}

func TestAPanicInATransactionFreesTheWriter(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	func() {
		defer func() { recover() }()
		storage.transaction(context.WithoutCancel(t.Context()), func(tx *sql.Tx) error {
			if _, err := tx.Exec(insertCounter); err != nil {
				t.Fatal(err)
			}
			panic("test: a bug inside a transaction")
		})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if count, err := storage.RateReserve(ctx, store.RateHit{Key: "k", Limit: 1}); err != nil || count == 0 {
		t.Fatalf("the writer stayed locked after a panic, or the panicking transaction committed: %d %v", count, err)
	}
}

func TestACommitAfterTheClientLeftReportsTheCancellation(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	err := storage.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(insertCounter); err != nil {
			return err
		}
		cancel()
		for {
			if _, err := tx.ExecContext(t.Context(), `SELECT 1`); errors.Is(err, sql.ErrTxDone) {
				return nil
			}
			time.Sleep(time.Millisecond)
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a commit after the client left: %v", err)
	}
}

func TestTheDummyRowGetsTheBlobWrapLengthOfALiveRow(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	earlier, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = earlier.writer.Exec(
		`UPDATE messages SET blob_wrap = ? WHERE locator = ?`,
		make([]byte, seal.WrapLen),
		store.DummyLocator[:],
	)
	if closeErr := earlier.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	dummy, err := reopened.Load(t.Context(), store.DummyLocator)
	if err != nil || len(dummy.BlobWrap) != seal.BlobWrapLen {
		t.Fatalf("the dummy row of an earlier release keeps a blob wrap of %d bytes: %v", len(dummy.BlobWrap), err)
	}
}

func TestABusyRetryStopsWhenItsCallerIsDone(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	attempts := 0
	err := retryBusy(ctx, func(context.Context) error {
		attempts++
		return ErrCheckpointBusy
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("a busy retry after its caller ended: %d attempts, %v", attempts, err)
	}
}

func TestDueBlobsFailWhenTheQueryCannotRun(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	page, err := storage.DueBlobs(ctx, time.Now(), seal.Locator{}, store.SweepBatch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("due blobs on a cancelled context: %+v %v", page, err)
	}
}

func TestACorruptRowLeavesTheRestOfItsPageToTheSweep(t *testing.T) {
	t.Parallel()
	storage, ctx := openTest(t), t.Context()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	insert := `INSERT INTO messages (locator, read_wrap, manage_wrap, record, blob_ref, blob_wrap, blob_purge_at, purge_at)
		VALUES (?, x'00', x'00', x'00', ?, x'00', ?, ?)`
	good := bytes.Repeat([]byte{2}, seal.LocatorLen)
	for locator, reference := range map[string]string{string([]byte{1, 2, 3}): "corrupt", string(good): "good"} {
		if _, err := storage.writer.Exec(
			insert,
			[]byte(locator),
			reference,
			now.UnixMilli(),
			now.UnixMilli(),
		); err != nil {
			t.Fatal(err)
		}
	}
	page, err := storage.DueBlobs(ctx, now, seal.Locator{}, store.SweepBatch)
	if err != nil || len(page) != 1 || page[0].Reference != "good" {
		t.Fatalf("a row with a corrupt locator must not hide the others: %+v %v", page, err)
	}
	if _, err = storage.Load(ctx, seal.Locator(good)); err != nil {
		t.Fatal(err)
	}
	visited := 0
	if err = storage.ResealWraps(ctx, func(*store.Row) bool { visited++; return false }); err != nil || visited != 2 {
		t.Fatalf("resealing must pass over a corrupt row and visit the good and the dummy row: %d %v", visited, err)
	}
	if purged, purgeErr := storage.PurgeRows(ctx, now, store.SweepBatch); purgeErr != nil || purged != 2 {
		t.Fatalf("the purge must remove a corrupt row too: %d %v", purged, purgeErr)
	}
}

func TestAScrubThatAnotherWriterOutgrowsIsBusy(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	scrubbed, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.writer.Exec(`CREATE TABLE padding (filler BLOB)`); err != nil {
		t.Fatal(err)
	}
	scrubbed.beforeScrubGrowth = func() {
		if _, writeErr := writer.writer.Exec(
			`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n
				WHERE i < (SELECT ifnull(max(slot), 0) + 100 FROM wal_scrub))
			INSERT INTO padding SELECT zeroblob(?) FROM n`,
			scrubFillerLen,
		); writeErr != nil {
			t.Error(writeErr)
		}
	}
	connection, err := scrubbed.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = scrubbed.scrubAll(t.Context(), connection)
	connection.Close()
	if !errors.Is(err, ErrCheckpointBusy) || !errors.Is(err, errScrubOutgrown) {
		t.Fatalf("scrub outgrown by another writer: %v, want ErrCheckpointBusy for the outgrown table", err)
	}
	if err = scrubbed.Close(); err != nil {
		t.Fatalf("Close while another writer outgrows the scrub: %v", err)
	}
}

func rowAt(locator seal.Locator) store.Row {
	row := store.NewDummyRow()
	row.Locator = locator
	return row
}

func refuse(t *testing.T, storage *Store, operation, table string) {
	t.Helper()
	if _, err := storage.writer.Exec(
		`CREATE TRIGGER refuse_` + strings.ToLower(operation) + `_` + table + ` BEFORE ` + operation + ` ON ` + table +
			` BEGIN SELECT RAISE(ABORT, 'refused'); END`,
	); err != nil {
		t.Fatal(err)
	}
}

func requireRefused(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestAWriteTheDatabaseRefusesFailsTheCall(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	storage := openTest(t)
	stored := rowAt(seal.Locator{1})
	if err := storage.Create(ctx, &stored); err != nil {
		t.Fatal(err)
	}
	refuse(t, storage, "INSERT", "rate_counters")
	hit := store.RateHit{Key: "k", WindowStart: time.Unix(0, 0), PurgeAt: time.Unix(60, 0)}
	_, err := storage.RateHit(ctx, []store.RateHit{hit})
	requireRefused(t, "a counter", err)
	counted := rowAt(seal.Locator{2})
	requireRefused(t, "a create that counts", storage.Create(ctx, &counted, hit))
	if _, err = storage.Load(ctx, counted.Locator); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a create whose counter failed stored its row: %v", err)
	}
	refuse(t, storage, "INSERT", "messages")
	fresh := rowAt(seal.Locator{3})
	requireRefused(t, "a create", storage.Create(ctx, &fresh))
	refuse(t, storage, "UPDATE", "messages")
	requireRefused(t, "a shred", storage.ShredBlobs(ctx, []store.DueBlob{{
		Locator:   stored.Locator,
		Reference: stored.BlobReference,
	}}))
	requireRefused(t, "a reseal", storage.ResealWraps(ctx, func(row *store.Row) bool {
		row.ReadWrap = bytes.Clone(row.ManageWrap)
		return true
	}))
	refuse(t, storage, "DELETE", "messages")
	_, err = storage.PurgeRows(ctx, time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC), store.SweepBatch)
	requireRefused(t, "a purge", err)
}

func TestOpenFailsWhenTheDummyRowCannotBeStored(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	earlier, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = earlier.Close(); err != nil {
		t.Fatal(err)
	}
	database := openPersistent(path, true)
	_, err = database.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	if closeErr := database.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	_, err = Open(t.Context(), path)
	requireRefused(t, "open", err)
}

func TestLookupAndLoadReportARowTheyCannotTrust(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	storage := openTest(t)
	known, absent := rowAt(seal.Locator{1}), rowAt(absentLocator)
	for _, row := range []*store.Row{&known, &absent} {
		if err := storage.Create(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := storage.Lookup(ctx, known.Locator); !errors.Is(err, store.ErrCorrupt) {
		t.Fatalf("a row at the locator no id hashes to: %v", err)
	}
	gone, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := storage.Lookup(gone, known.Locator); !errors.Is(err, context.Canceled) {
		t.Fatalf("a lookup whose caller left: %v", err)
	}
	if _, err := storage.KnownBlobReferences(gone); !errors.Is(err, context.Canceled) {
		t.Fatalf("the blob references for a caller that left: %v", err)
	}
	if err := storage.ResealWraps(gone, func(*store.Row) bool { return false }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a reseal for a caller that left: %v", err)
	}
	if _, err := storage.writer.Exec(
		`UPDATE messages SET purge_at = 'later' WHERE locator = ?`,
		known.Locator[:],
	); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Load(ctx, known.Locator); err == nil {
		t.Fatal("a purge time that is no number was loaded")
	}
}

func TestReadRowsStopsAtARowItCannotRead(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	rows, err := storage.reader.QueryContext(t.Context(), `SELECT locator FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("refused")
	if values, readErr := readRows(rows, func(*sql.Rows) (int, error) { return 0, refused }); values != nil ||
		!errors.Is(readErr, refused) {
		t.Fatalf("a row it cannot read: %v %v", values, readErr)
	}
}

func longPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), strings.Repeat("n", 250)+".db")
}

func TestOpenAndInspectRefuseWhatTheyCannotRead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	if _, err := Open(ctx, longPath(t)); !errors.Is(err, syscall.ENAMETOOLONG) {
		t.Fatalf("a database whose log cannot be named: %v", err)
	}
	if _, err := Inspect(ctx, longPath(t)); err != nil {
		t.Fatalf("a missing database with a long name: %v", err)
	}
	long := longPath(t)
	if err := os.WriteFile(long, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, long); !errors.Is(err, syscall.ENAMETOOLONG) {
		t.Fatalf("inspecting a database whose log cannot be named: %v", err)
	}
	directory := t.TempDir()
	if _, err := Open(ctx, filepath.Join(directory, "missing", "nylzo.db")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a database in a missing directory: %v", err)
	}
	file := filepath.Join(directory, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, filepath.Join(file, "nylzo.db")); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("a database below a file: %v", err)
	}
	if readiness, err := Inspect(ctx, file); readiness != ReadinessCreate || err != nil {
		t.Fatalf("an empty file without a log: %v %v", readiness, err)
	}
	malformed := filepath.Join(directory, "malformed.db")
	database, err := sql.Open("sqlite", "file:"+malformed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`CREATE TABLE padding (filler BLOB)`)
	if closeErr := database.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	content, err := os.ReadFile(malformed)
	if err != nil {
		t.Fatal(err)
	}
	copy(content[100:], bytes.Repeat([]byte{0xff}, 12))
	if err = os.WriteFile(malformed, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(ctx, malformed); err == nil {
		t.Fatal("a database whose schema page is malformed was inspected")
	}
	if _, err = OpenCurrent(ctx, malformed); err == nil {
		t.Fatal("a database whose schema page is malformed was opened")
	}
}

func dropTable(t *testing.T, path, table string) {
	t.Helper()
	database := openPersistent(path, true)
	_, err := database.Exec(`DROP TABLE ` + table)
	if closeErr := database.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}

func TestOpenRefusesADatabaseThatLacksATable(t *testing.T) {
	t.Parallel()
	for table, open := range map[string]func(context.Context, string) (*Store, error){
		"messages":      OpenCurrent,
		"rate_counters": Open,
	} {
		path := filepath.Join(t.TempDir(), "nylzo.db")
		earlier, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		if err = earlier.Close(); err != nil {
			t.Fatal(err)
		}
		dropTable(t, path, table)
		if _, err = open(t.Context(), path); err == nil || !strings.Contains(err.Error(), "no such table") {
			t.Fatalf("a database without %s: %v", table, err)
		}
	}
}

func TestATransactionForACallerThatLeftCommitsNothing(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	connection, err := storage.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	ran := false
	if err = commit(
		gone,
		connection,
		func(*sql.Tx) error { ran = true; return nil },
	); !errors.Is(
		err,
		context.Canceled,
	) ||
		ran {
		t.Fatalf("a commit for a caller that left: %v %v", ran, err)
	}
}

func TestARebuildThatFailsIsTriedAgain(t *testing.T) {
	t.Parallel()
	empty := &Store{
		writer: openPersistent(filepath.Join(t.TempDir(), "empty.db"), true),
		scrubs: make(chan struct{}, 1),
	}
	defer empty.writer.Close()
	empty.changedSinceBuild.Store(true)
	if err := empty.rebuildChanged(t.Context()); err == nil || !empty.changedSinceBuild.Load() {
		t.Fatalf("a rebuild without tables: %v", err)
	}
}

func busyError(t *testing.T) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	holder := openPersistent(path, true)
	t.Cleanup(func() { holder.Close() })
	held, err := holder.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { held.Rollback() })
	if _, err = held.Exec(`CREATE TABLE held (x)`); err != nil {
		t.Fatal(err)
	}
	impatient, err := sql.Open("sqlite", "file:"+path+"?_busy_timeout=0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { impatient.Close() })
	_, err = impatient.Exec(`CREATE TABLE waiting (x)`)
	if !busy(err) {
		t.Fatalf("no busy error: %v", err)
	}
	return err
}

func TestAHeldWriteLockReadsAsABusyCheckpoint(t *testing.T) {
	t.Parallel()
	held := busyError(t)
	if !errors.Is(wrapRebuild(held), ErrCheckpointBusy) || !errors.Is(wrapScrub(held), ErrCheckpointBusy) {
		t.Fatal("a held write lock must let the next sweep retry")
	}
	other := errors.New("other")
	if errors.Is(wrapRebuild(other), ErrCheckpointBusy) || errors.Is(wrapScrub(other), ErrCheckpointBusy) {
		t.Fatal("only a held write lock is busy")
	}
}

func TestAScrubOnAConnectionThatWentFails(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	connection, err := storage.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	for name, err := range map[string]error{
		"the timeout":   withCheckpointTimeout(t.Context(), connection, func() error { return nil }),
		"a full scrub":  storage.scrubAll(t.Context(), connection),
		"a restart":     restartLog(t.Context(), connection),
		"a rewrite":     rewriteSlots(t.Context(), connection, 1),
		"the first one": storage.scrubFirst(t.Context(), connection),
	} {
		if !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("%s: %v", name, err)
		}
	}
	connection, err = storage.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = withCheckpointTimeout(t.Context(), connection, func() error { return connection.Close() })
	if !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("a timeout that cannot be restored: %v", err)
	}
	if ignoreBadConn(sqldriver.ErrBadConn) != nil || ignoreBadConn(sql.ErrConnDone) == nil {
		t.Fatal("only a connection that database/sql discards is no error")
	}
}

func TestTheScrubNeedsTheLogItCounts(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	connection, err := storage.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	misnamed := &Store{path: longPath(t), pageSize: storage.pageSize}
	if err = misnamed.scrubAll(t.Context(), connection); !errors.Is(err, syscall.ENAMETOOLONG) {
		t.Fatalf("a log it cannot stat: %v", err)
	}
	if frames, framesErr := (&Store{path: filepath.Join(t.TempDir(), "missing.db")}).walFrames(); frames != 0 ||
		framesErr != nil {
		t.Fatalf("a missing log: %d %v", frames, framesErr)
	}
	if _, err = connection.ExecContext(t.Context(), `DROP TABLE wal_scrub`); err != nil {
		t.Fatal(err)
	}
	if err = storage.scrubAll(t.Context(), connection); err == nil || errors.Is(err, ErrCheckpointBusy) {
		t.Fatalf("a scrub without its table: %v", err)
	}
}

func TestAScrubThatNeverCatchesUpIsShort(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	connection, err := storage.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	slots, err := scrubSlots(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "growing.db")
	frames := int64(slots + writeScrubFrames + 1)
	grow := func() {
		size := walHeaderLen + frames*int64(walFrameHeaderLen+storage.pageSize)
		if truncateErr := os.WriteFile(log+"-wal", nil, 0o600); truncateErr != nil {
			t.Error(truncateErr)
		}
		if truncateErr := os.Truncate(log+"-wal", size); truncateErr != nil {
			t.Error(truncateErr)
		}
		frames *= 2
	}
	grow()
	growing := &Store{path: log, pageSize: storage.pageSize, beforeScrubGrowth: grow}
	if err = growing.scrubAll(t.Context(), connection); !errors.Is(err, errScrubShort) {
		t.Fatalf("a log that outgrows every growth: %v", err)
	}
}

func TestAFailedScrubAfterAWriteIsReported(t *testing.T) {
	t.Parallel()
	storage := openTest(t)
	reported := make(chan error, 1)
	storage.OnScrubFailure(func(err error) {
		select {
		case reported <- err:
		default:
		}
	})
	if _, err := storage.writer.Exec(`DROP TABLE wal_scrub`); err != nil {
		t.Fatal(err)
	}
	row := rowAt(seal.Locator{1})
	if err := storage.Create(t.Context(), &row); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reported:
		if err == nil || errors.Is(err, ErrCheckpointBusy) {
			t.Fatalf("reported %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the failed scrub was not reported")
	}
}

func TestTheLogResetSkipsWhatItCannotCheckpoint(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	database := openPersistent(filepath.Join(directory, "any.db"), true)
	defer database.Close()
	if _, err := checkpointAndZero(
		gone,
		database,
		filepath.Join(directory, "any.db"),
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("a reset for a caller that left: %v", err)
	}
	live := filepath.Join(directory, "live.db")
	storage, err := Open(t.Context(), live)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	row := rowAt(seal.Locator{1})
	if err = storage.Create(t.Context(), &row); err != nil {
		t.Fatal(err)
	}
	if skipped, resetErr := resetLog(t.Context(), live); !skipped || resetErr != nil {
		t.Fatalf("a database another connection has open: %v %v", skipped, resetErr)
	}
	garbage := filepath.Join(directory, "garbage.db")
	for _, path := range []string{garbage, garbage + "-wal"} {
		if err = os.WriteFile(path, bytes.Repeat([]byte("not a database "), 64), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if skipped, resetErr := resetLog(t.Context(), garbage); resetErr == nil && !skipped {
		t.Fatal("a log beside a file that is no database was zeroed")
	}
}

var errBare = errors.New("bare driver")

type bareConnection struct{}

func (bareConnection) Prepare(string) (sqldriver.Stmt, error) { return nil, errBare }

func (bareConnection) Close() error { return nil }

func (bareConnection) Begin() (sqldriver.Tx, error) { return nil, errBare }

type unpersistedConnection struct{ bareConnection }

func (unpersistedConnection) FileControlPersistWAL(string, int) (int, error) { return 0, errBare }

func (unpersistedConnection) FileControlDataVersion(string) (uint32, error) { return 0, errBare }

type bareConnector struct {
	connection sqldriver.Conn
	err        error
}

func (c bareConnector) Connect(context.Context) (sqldriver.Conn, error) { return c.connection, c.err }

func (bareConnector) Driver() sqldriver.Driver { return nil }

func TestADriverWithoutTheControlsTheStoreNeedsIsRefused(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		connector bareConnector
		want      error
	}{
		"a connection that fails":        {bareConnector{err: errBare}, errBare},
		"a log that would be freed":      {bareConnector{connection: bareConnection{}}, errNoFileControl},
		"a log that cannot be persisted": {bareConnector{connection: unpersistedConnection{}}, errBare},
	} {
		if _, err := (persistentLog{Connector: testCase.connector}).Connect(
			t.Context(),
		); !errors.Is(
			err,
			testCase.want,
		) {
			t.Errorf("%s: %v", name, err)
		}
	}
	database := sql.OpenDB(bareConnector{connection: bareConnection{}})
	defer database.Close()
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err = writtenPages(connection); !errors.Is(err, errNoStatus) {
		t.Fatalf("a driver that cannot count written pages: %v", err)
	}
}
