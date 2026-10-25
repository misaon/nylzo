package sqlite

import (
	"cmp"
	"context"
	crand "crypto/rand"
	"database/sql"
	sqldriver "database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/url"
	"os"
	"strings"
	"time"

	driver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/misaon/nylzo/internal/store"
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

func revealingCounter(row string) string {
	return fmt.Sprintf(
		"%[1]skey GLOB 'create:*' OR %[1]skey GLOB 'read:*' OR %[1]skey GLOB 'abuse:h:*' OR %[1]skey GLOB 'abuse:id:*:*' OR (%[1]skey GLOB 'pow:*' AND %[1]swindow_start != 0)",
		row,
	)
}

const (
	readerConns   = 8
	openRetryFor  = 10 * time.Second
	openBackoff   = 20 * time.Millisecond
	openMaxWait   = 200 * time.Millisecond
	schemaVersion = 4
	maxCacheKiB   = 512
	busyMS        = 5000
	checkpointMS  = 250
	minLeaseRowID = 1 << 32
)

func requireCurrent(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	switch {
	case version > schemaVersion:
		return fmt.Errorf("%w: version %d, this build knows %d", ErrNewerSchema, version, schemaVersion)
	case version < schemaVersion:
		return fmt.Errorf("%w: version %d, this build needs %d", ErrOlderSchema, version, schemaVersion)
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("%w: version %d, this build knows %d", ErrNewerSchema, version, schemaVersion)
	}
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	if err := migrateTables(ctx, conn, version); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	if version >= schemaVersion {
		return nil
	}
	if _, err := conn.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	return err
}

func migrateTables(ctx context.Context, conn *sql.Conn, version int) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var has int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name = 'ending'`).
		Scan(&has); err != nil {
		return err
	}
	if has == 0 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN ending TEXT`); err != nil {
			return err
		}
	}
	if err := dropPerAddressAttempts(ctx, tx); err != nil {
		return err
	}
	for _, t := range unorderedTables {
		if err := dropRowid(ctx, tx, t); err != nil {
			return err
		}
	}
	if version < schemaVersion {
		if _, err := tx.ExecContext(ctx, `DELETE FROM rate_counters WHERE `+revealingCounter("")); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, indexes+guards); err != nil {
		return err
	}
	var violations int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		return err
	}
	if violations != 0 {
		return fmt.Errorf("sqlite: migration left %d foreign key violations", violations)
	}
	return tx.Commit()
}

var (
	ErrNewerSchema = errors.New("sqlite: the database was written by a newer nylzo")
	ErrOlderSchema = errors.New("sqlite: the database has an older schema; start nylzo serve to migrate it")
)

func dropPerAddressAttempts(ctx context.Context, tx *sql.Tx) error {
	columns := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info('messages')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	var stmts []string
	if !columns["next_attempt_at"] {
		stmts = append(stmts, `ALTER TABLE messages ADD COLUMN next_attempt_at INTEGER NOT NULL DEFAULT 0`)
	}
	for _, c := range []string{"day_fail_count", "day_fail_date"} {
		if columns[c] {
			stmts = append(stmts, `ALTER TABLE messages DROP COLUMN `+c)
		}
	}
	stmts = append(stmts, `DROP TABLE IF EXISTS passphrase_attempts`)
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

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

func dropRowid(ctx context.Context, tx *sql.Tx, t unorderedTable) error {
	var ddl string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, t.name).
		Scan(&ddl); err != nil {
		return err
	}
	if strings.HasSuffix(strings.TrimSpace(ddl), "WITHOUT ROWID") {
		return nil
	}
	rebuilt := t.name + "_rebuilt"
	create := strings.Replace(ddl, t.name, rebuilt, 1) + " WITHOUT ROWID"
	for _, stmt := range []string{
		create,
		fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s`, rebuilt, t.columns, t.columns, t.name),
		fmt.Sprintf(`DROP TABLE %s`, t.name),
		fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, rebuilt, t.name),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_%s ON %s(%s)`, t.name, t.index, t.name, t.index),
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

type Store struct {
	w    *sql.DB
	r    *sql.DB
	stmt *statements
}

type statements struct {
	get, leases          *sql.Stmt
	record, hit, reserve *sql.Stmt
}

func prepare(ctx context.Context, w, r *sql.DB) (*statements, error) {
	st := &statements{}
	for _, p := range []struct {
		db    *sql.DB
		stmt  **sql.Stmt
		query string
	}{
		{r, &st.get, recordQuery},
		{r, &st.leases, leasesQuery},
		{w, &st.record, recordQuery},
		{w, &st.hit, hitQuery},
		{w, &st.reserve, reserveQuery},
	} {
		stmt, err := p.db.PrepareContext(ctx, p.query)
		if err != nil {
			return nil, errors.Join(err, st.close())
		}
		*p.stmt = stmt
	}
	return st, nil
}

func (st *statements) close() error {
	var errs []error
	for _, stmt := range []*sql.Stmt{st.get, st.leases, st.record, st.hit, st.reserve} {
		if stmt != nil {
			errs = append(errs, stmt.Close())
		}
	}
	return errors.Join(errs...)
}

var _ store.Store = (*Store)(nil)

func dsn(path string, write bool) string {
	q := url.Values{}
	if write {
		q.Set("_txlock", "immediate")
	}
	q.Set("_busy_timeout", fmt.Sprint(busyMS))
	q.Set("_journal_mode", "WAL")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "secure_delete(1)")
	q.Add("_pragma", "journal_size_limit(0)")
	q.Add("_pragma", "wal_autocheckpoint(1)")
	q.Add("_pragma", fmt.Sprintf("cache_size(-%d)", maxCacheKiB))
	q.Add("_pragma", "temp_store(2)")
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

func Open(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, migrate)
}

func OpenCurrent(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, requireCurrent)
}

func openWith(ctx context.Context, path string, prepareSchema func(context.Context, *sql.DB) error) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	deadline := time.Now().Add(openRetryFor)
	for attempt := 1; ; attempt++ {
		s, err := open(ctx, path, prepareSchema)
		if err == nil || !busy(err) || time.Now().After(deadline) {
			return s, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(min(time.Duration(attempt)*openBackoff, openMaxWait) + rand.N(openBackoff)):
		}
	}
}

func busy(err error) bool {
	var e *driver.Error
	return errors.As(err, &e) && e.Code()&0xff == sqlite3.SQLITE_BUSY
}

func open(ctx context.Context, path string, prepareSchema func(context.Context, *sql.DB) error) (*Store, error) {
	w, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := prepareSchema(ctx, w); err != nil {
		w.Close()
		return nil, fmt.Errorf("sqlite: schema: %w", err)
	}
	r, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(readerConns)
	r.SetMaxIdleConns(readerConns)
	if err := r.PingContext(ctx); err != nil {
		r.Close()
		w.Close()
		return nil, err
	}
	stmt, err := prepare(ctx, w, r)
	if err != nil {
		r.Close()
		w.Close()
		return nil, err
	}
	return &Store{w: w, r: r, stmt: stmt}, nil
}

func (s *Store) Close() error {
	return errors.Join(s.stmt.close(), s.r.Close(), s.w.Close())
}

func ms(t time.Time) int64 {
	return t.UnixMilli()
}

func fromMS(v int64) time.Time {
	return time.UnixMilli(v).UTC()
}

func optTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMS(v.Int64)
	return &t
}

func (s *Store) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return cmp.Or(ctx.Err(), err)
	}
	return nil
}

func (s *Store) Create(ctx context.Context, m *store.Message, counters ...store.RateHit) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT id FROM messages WHERE id = ? UNION ALL SELECT id FROM tombstones WHERE id = ?)`, m.ID, m.ID).
			Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			return store.ErrConflict
		}
		var verifier any
		if len(m.VerifierMAC) > 0 {
			verifier = m.VerifierMAC
		}
		var blobRef any
		if m.BlobRef != "" {
			blobRef = m.BlobRef
		}
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO messages (id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, verifier_mac, size, blob_ref)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID,
			m.Version,
			m.KDF,
			m.Flags,
			string(store.StateAvailable),
			ms(m.CreatedAt),
			ms(m.ExpiresAt),
			m.TTLSeconds,
			m.OpensMax,
			m.OpensMax,
			m.PepperID,
			m.ReadMAC,
			m.ManageMAC,
			verifier,
			m.Size,
			blobRef,
		)
		if err != nil {
			return err
		}
		if len(counters) == 0 {
			return nil
		}
		stmt := tx.StmtContext(ctx, s.stmt.hit)
		for _, h := range counters {
			if _, err := hit(ctx, stmt, h); err != nil {
				return err
			}
		}
		return nil
	})
}

const recordQuery = `SELECT 'm' AS kind, id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, verifier_mac, size, blob_ref, fail_count_total, next_attempt_at, first_opened_at, NULL AS ended_at, NULL AS purge_at, ending FROM messages WHERE id = ?
UNION ALL
SELECT 't', id, 0, 0, 0, state, 0, 0, 0, 0, 0, pepper_id, read_mac, manage_mac, NULL, 0, NULL, 0, 0, first_opened_at, ended_at, purge_at, NULL FROM tombstones WHERE id = ?`

func scanRecord(row *sql.Row) (store.Record, error) {
	var (
		kind                                    string
		m                                       store.Message
		state                                   string
		createdAt, expiresAt                    int64
		verifier                                []byte
		blobRef, ending                         sql.NullString
		firstOpened, endedAt, purgeAt           sql.NullInt64
		version, kdf, flags, opensMax, opensRem int
		pepperID, ttl, size, total, nextAttempt int64
	)
	err := row.Scan(
		&kind,
		&m.ID,
		&version,
		&kdf,
		&flags,
		&state,
		&createdAt,
		&expiresAt,
		&ttl,
		&opensMax,
		&opensRem,
		&pepperID,
		&m.ReadMAC,
		&m.ManageMAC,
		&verifier,
		&size,
		&blobRef,
		&total,
		&nextAttempt,
		&firstOpened,
		&endedAt,
		&purgeAt,
		&ending,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Record{}, store.ErrNotFound
	}
	if err != nil {
		return store.Record{}, err
	}
	if kind != "t" && (opensMax < 1 || opensMax > math.MaxUint16 || opensRem < 0 || opensRem > opensMax) {
		return store.Record{}, fmt.Errorf("%w: opens %d of %d", errCorruptRow, opensRem, opensMax)
	}
	if kind == "t" {
		return store.Record{
			Tombstone: &store.Tombstone{
				ID:            m.ID,
				PepperID:      byte(pepperID),
				ReadMAC:       m.ReadMAC,
				ManageMAC:     m.ManageMAC,
				State:         store.State(state),
				FirstOpenedAt: optTime(firstOpened),
				EndedAt:       fromMS(endedAt.Int64).Truncate(time.Minute),
				PurgeAt:       fromMS(purgeAt.Int64),
			},
		}, nil
	}
	m.Version = uint8(version)
	m.KDF = uint8(kdf)
	m.Flags = uint8(flags)
	m.State = store.State(state)
	m.CreatedAt = fromMS(createdAt)
	m.ExpiresAt = fromMS(expiresAt)
	m.TTLSeconds = uint32(ttl)
	m.OpensMax = uint16(opensMax)
	m.OpensRemaining = uint16(opensRem)
	m.PepperID = byte(pepperID)
	m.VerifierMAC = verifier
	m.Size = size
	m.BlobRef = blobRef.String
	m.FailCountTotal = int(total)
	if nextAttempt > 0 {
		m.NextAttemptAt = fromMS(nextAttempt)
	}
	m.FirstOpenedAt = optTime(firstOpened)
	m.Ending = store.State(ending.String)
	return store.Record{Message: &m}, nil
}

func (s *Store) Get(ctx context.Context, id string) (store.Record, error) {
	return scanRecord(s.stmt.get.QueryRowContext(ctx, id, id))
}

func (s *Store) recordIn(ctx context.Context, tx *sql.Tx, id string) (store.Record, error) {
	return scanRecord(tx.StmtContext(ctx, s.stmt.record).QueryRowContext(ctx, id, id))
}

func attemptMS(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return ms(t)
}

func backoff(failCount int) time.Duration {
	if failCount <= store.FreeAttempts {
		return 0
	}
	return min(store.MaxBackoff, time.Second<<min(failCount-store.FreeAttempts, 20))
}

var (
	errCorruptRow   = errors.New("sqlite: a message row holds impossible open counters")
	errClaimParams  = errors.New("sqlite: claim needs a clock, and a verifier check when a verifier is sent")
	errNoLeaseRowID = errors.New("sqlite: no free lease row id")
)

func (s *Store) Claim(ctx context.Context, p store.ClaimParams) (store.ClaimResult, error) {
	if p.Clock == nil || (p.VerifierSent && p.VerifyMAC == nil) {
		return store.ClaimResult{}, errClaimParams
	}
	var result store.ClaimResult
	var passErr *store.PassphraseError
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := p.Clock()
		rec, err := s.recordIn(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if rec.Tombstone != nil {
			return &store.GoneError{State: rec.Tombstone.State}
		}
		m := rec.Message
		if m.Ending != "" {
			return &store.GoneError{State: m.Ending}
		}
		if m.State != store.StateAvailable {
			return &store.GoneError{State: m.State}
		}
		if m.OpensRemaining == 0 {
			return &store.GoneError{State: store.StateOpened}
		}
		if !now.Before(m.ExpiresAt) {
			return &store.GoneError{State: store.StateExpired}
		}
		if m.PassphraseRequired() {
			if m.FailCountTotal >= store.MaxFailedAttempts {
				passErr = &store.PassphraseError{RetryAfter: m.ExpiresAt.Sub(now)}
				return nil
			}
			if m.NextAttemptAt.After(now) {
				passErr = &store.PassphraseError{RetryAfter: m.NextAttemptAt.Sub(now)}
				return nil
			}
			if !p.VerifierSent || !p.VerifyMAC(m.PepperID, m.VerifierMAC) {
				total := m.FailCountTotal + 1
				wait := backoff(total)
				var next time.Time
				if wait > 0 {
					next = store.CeilMinute(now.Add(wait))
					wait = next.Sub(now)
				}
				if _, err := tx.ExecContext(
					ctx,
					`UPDATE messages SET fail_count_total = ?, next_attempt_at = ? WHERE id = ?`,
					total,
					attemptMS(next),
					m.ID,
				); err != nil {
					return err
				}
				passErr = &store.PassphraseError{RetryAfter: wait, AttemptsLeft: max(0, store.FreeAttempts-total)}
				return nil
			}
		}
		m.OpensRemaining--
		if m.OpensRemaining == 0 {
			m.State = store.StateOpened
		}
		if m.FirstOpenedAt == nil {
			t := now.UTC().Truncate(time.Minute)
			m.FirstOpenedAt = &t
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE messages SET opens_remaining = ?, state = ?, first_opened_at = ? WHERE id = ?`,
			m.OpensRemaining,
			string(m.State),
			ms(*m.FirstOpenedAt),
			m.ID,
		); err != nil {
			return err
		}
		expires := store.CeilMinute(now.Add(store.LeaseFor(m.Size)))
		rowID, err := insertLease(ctx, tx, m.ID, p, expires)
		if err != nil {
			return err
		}
		result = store.ClaimResult{
			Message: m,
			Lease: store.Lease{
				RowID:     rowID,
				ID:        m.ID,
				MAC:       p.LeaseMAC,
				PepperID:  p.LeasePepperID,
				ExpiresAt: expires,
			},
		}
		return nil
	})
	if err != nil {
		return store.ClaimResult{}, err
	}
	if passErr != nil {
		return store.ClaimResult{}, passErr
	}
	return result, nil
}

func (s *Store) Leases(ctx context.Context, id string) ([]store.Lease, error) {
	rows, err := s.stmt.leases.QueryContext(ctx, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Lease
	for rows.Next() {
		var l store.Lease
		var pepperID, expires int64
		if err := rows.Scan(&l.RowID, &l.ID, &l.MAC, &pepperID, &expires, &l.Downloads); err != nil {
			return nil, err
		}
		l.PepperID = byte(pepperID)
		l.ExpiresAt = fromMS(expires)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) RecordDownload(ctx context.Context, l store.Lease, now time.Time) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var expires int64
		var downloads int
		var ending sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT l.lease_expires_at, l.downloads, m.ending FROM leases l JOIN messages m ON m.id = l.id WHERE l.row_id = ? AND l.id = ? AND l.lease_mac = ?`, l.RowID, l.ID, l.MAC).
			Scan(&expires, &downloads, &ending)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && expires <= ms(now)) {
			return store.ErrLeaseInvalid
		}
		if err != nil {
			return err
		}
		if ending.Valid {
			return &store.GoneError{State: store.State(ending.String)}
		}
		if downloads >= store.MaxLeaseDownload {
			return store.ErrLeaseLimit
		}
		_, err = tx.ExecContext(ctx, `UPDATE leases SET downloads = ? WHERE row_id = ?`, downloads+1, l.RowID)
		return err
	})
}

func (s *Store) Ack(ctx context.Context, l store.Lease, now time.Time) (string, error) {
	var ref string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var ending sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT m.ending FROM leases l JOIN messages m ON m.id = l.id WHERE l.row_id = ? AND l.id = ? AND l.lease_mac = ?`, l.RowID, l.ID, l.MAC).
			Scan(&ending)
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrLeaseInvalid
		}
		if err != nil {
			return err
		}
		if ending.Valid {
			return &store.GoneError{State: store.State(ending.String)}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM leases WHERE row_id = ?`, l.RowID); err != nil {
			return err
		}
		ref, err = releaseInTx(ctx, tx, l.ID, now)
		return err
	})
	return ref, err
}

func (s *Store) ReleaseIfDone(ctx context.Context, id string, now time.Time) (string, error) {
	var ref string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		ref, err = releaseInTx(ctx, tx, id, now)
		return err
	})
	return ref, err
}

func releaseInTx(ctx context.Context, tx *sql.Tx, id string, now time.Time) (string, error) {
	var opensRemaining int
	var blobRef sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT opens_remaining, blob_ref FROM messages WHERE id = ?`, id).
		Scan(&opensRemaining, &blobRef)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if opensRemaining != 0 || !blobRef.Valid || blobRef.String == "" {
		return "", nil
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM leases WHERE id = ? AND lease_expires_at > ?`, id, ms(now)).
		Scan(&active); err != nil {
		return "", err
	}
	if active > 0 {
		return "", nil
	}
	_, err = tx.ExecContext(
		ctx,
		`UPDATE messages SET ending = ? WHERE id = ? AND ending IS NULL AND expires_at > ?`,
		string(store.StateOpened),
		id,
		ms(now),
	)
	return blobRef.String, err
}

func (s *Store) ClearBlob(ctx context.Context, id, blobRef string, now time.Time) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		rec, err := s.recordIn(ctx, tx, id)
		if errors.Is(err, store.ErrNotFound) || (err == nil && (rec.Message == nil || rec.Message.BlobRef != blobRef)) {
			return nil
		}
		if err != nil {
			return err
		}
		return endInTx(ctx, tx, rec.Message, endingOr(rec.Message, store.StateOpened), now)
	})
}

func endingOr(m *store.Message, state store.State) store.State {
	if m.Ending != "" {
		return m.Ending
	}
	return state
}

func (s *Store) MarkEnding(
	ctx context.Context,
	id string,
	state store.State,
	now time.Time,
) (store.State, string, bool, error) {
	var ending store.State
	var ref string
	recorded := false
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rec, err := s.recordIn(ctx, tx, id)
		if err != nil {
			return err
		}
		m := rec.Message
		if m == nil {
			return store.ErrNotFound
		}
		ref = m.BlobRef
		expired := !now.Before(m.ExpiresAt)
		switch {
		case m.Ending != "":
			ending = m.Ending
			return nil
		case m.OpensRemaining == 0 && (expired || state == store.StateExpired):
			ending = store.StateOpened
		case expired:
			ending = store.StateExpired
		default:
			ending = state
		}
		if expired {
			return nil
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE messages SET ending = ? WHERE id = ?`,
			string(ending),
			id,
		); err != nil {
			return err
		}
		recorded = true
		return nil
	})
	return ending, ref, recorded && err == nil, err
}

func (s *Store) End(ctx context.Context, id string, state store.State, now time.Time) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		rec, err := s.recordIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if rec.Message == nil {
			return store.ErrNotFound
		}
		return endInTx(ctx, tx, rec.Message, endingOr(rec.Message, state), now)
	})
}

func endInTx(ctx context.Context, tx *sql.Tx, m *store.Message, state store.State, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, m.ID); err != nil {
		return err
	}
	var firstOpened any
	if m.FirstOpenedAt != nil {
		firstOpened = ms(*m.FirstOpenedAt)
	}
	ended := now
	if !now.Before(m.ExpiresAt) {
		ended = m.ExpiresAt
	}
	ended = ended.UTC().Truncate(time.Minute)
	_, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO tombstones (id, pepper_id, read_mac, manage_mac, state, first_opened_at, ended_at, purge_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID,
		m.PepperID,
		m.ReadMAC,
		m.ManageMAC,
		string(state),
		firstOpened,
		ms(ended),
		ms(ended.Add(store.TombstoneTTL)),
	)
	return err
}

func (s *Store) RateHit(ctx context.Context, hits []store.RateHit) ([]store.RateCount, error) {
	out := make([]store.RateCount, 0, len(hits))
	err := s.tx(ctx, func(tx *sql.Tx) error {
		stmt := tx.StmtContext(ctx, s.stmt.hit)
		for _, h := range hits {
			c, err := hit(ctx, stmt, h)
			if err != nil {
				return err
			}
			out = append(out, c)
			if h.Over(c) {
				return nil
			}
		}
		return nil
	})
	return out, err
}

const (
	leasesQuery = `SELECT row_id, id, lease_mac, pepper_id, lease_expires_at, downloads FROM leases WHERE id = ? ORDER BY row_id`
	hitQuery    = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES (?, ?, 1, ?, ?)
		ON CONFLICT(key, window_start) DO UPDATE SET count = count + 1, bytes = bytes + excluded.bytes RETURNING count, bytes`
	reserveQuery = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES (?, ?, 1, 0, ?)
		ON CONFLICT(key, window_start) DO UPDATE SET count = count + 1 WHERE count < ? RETURNING count`
)

func hit(ctx context.Context, stmt *sql.Stmt, h store.RateHit) (store.RateCount, error) {
	var c store.RateCount
	err := stmt.QueryRowContext(ctx, h.Key, ms(h.WindowStart), h.Bytes, ms(h.PurgeAt)).Scan(&c.Count, &c.Bytes)
	return c, err
}

func (s *Store) RateReserve(ctx context.Context, h store.RateHit) (int64, bool, error) {
	return reserve(ctx, s.stmt.reserve, h)
}

func (s *Store) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	reserved := make([]bool, len(hits))
	err := s.tx(ctx, func(tx *sql.Tx) error {
		stmt := tx.StmtContext(ctx, s.stmt.reserve)
		for i, h := range hits {
			_, ok, err := reserve(ctx, stmt, h)
			if err != nil {
				return err
			}
			reserved[i] = ok
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return reserved, nil
}

func reserve(ctx context.Context, stmt *sql.Stmt, h store.RateHit) (int64, bool, error) {
	var count int64
	err := stmt.QueryRowContext(ctx, h.Key, ms(h.WindowStart), ms(h.PurgeAt), h.Limit).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return count, err == nil, err
}

func (s *Store) RateRelease(ctx context.Context, key string, windowStart time.Time) error {
	_, err := s.w.ExecContext(
		ctx,
		`UPDATE rate_counters SET count = count - 1 WHERE key = ? AND window_start = ? AND count > 0`,
		key,
		ms(windowStart),
	)
	return err
}

func (s *Store) RateGet(ctx context.Context, key string, windowStart time.Time) (store.RateCount, error) {
	var c store.RateCount
	err := s.r.QueryRowContext(ctx, `SELECT count, bytes FROM rate_counters WHERE key = ? AND window_start = ?`, key, ms(windowStart)).
		Scan(&c.Count, &c.Bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RateCount{}, nil
	}
	return c, err
}

func (s *Store) ExpiredPage(
	ctx context.Context,
	now time.Time,
	after store.ExpiredMessage,
	limit int,
) ([]store.ExpiredMessage, error) {
	rows, err := s.r.QueryContext(
		ctx,
		`SELECT id, blob_ref, expires_at FROM messages WHERE expires_at <= ? AND (expires_at, id) > (?, ?) ORDER BY expires_at, id LIMIT ?`,
		ms(now),
		ms(after.ExpiresAt),
		after.ID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ExpiredMessage
	for rows.Next() {
		var m store.ExpiredMessage
		var ref sql.NullString
		var expires int64
		if err := rows.Scan(&m.ID, &ref, &expires); err != nil {
			return nil, err
		}
		m.BlobRef = ref.String
		m.ExpiresAt = fromMS(expires)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) EndExpired(ctx context.Context, ids []string, now time.Time) (int, error) {
	var ended int
	var errs []error
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ended, errs = 0, nil
		for _, id := range ids {
			ok, err := s.endExpiredInTx(ctx, tx, id, now)
			if errors.Is(err, errCorruptRow) {
				errs = append(errs, fmt.Errorf("sqlite: end expired: %w", err))
				continue
			}
			if err != nil {
				return err
			}
			if ok {
				ended++
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("sqlite: end expired: %w", err)
	}
	return ended, errors.Join(errs...)
}

func (s *Store) endExpiredInTx(ctx context.Context, tx *sql.Tx, id string, now time.Time) (bool, error) {
	rec, err := s.recordIn(ctx, tx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && rec.Message == nil) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	m := rec.Message
	if now.Before(m.ExpiresAt) {
		return false, nil
	}
	ending := store.StateExpired
	if m.OpensRemaining == 0 {
		ending = store.StateOpened
	}
	return true, endInTx(ctx, tx, m, endingOr(m, ending), now)
}

func (s *Store) SpentPage(ctx context.Context, after string, limit int) ([]string, error) {
	return s.ids(
		ctx,
		`SELECT id FROM messages WHERE opens_remaining = 0 AND blob_ref IS NOT NULL AND id > ? ORDER BY id LIMIT ?`,
		after,
		limit,
	)
}

func (s *Store) EndingPage(ctx context.Context, now time.Time, after string, limit int) ([]string, error) {
	return s.ids(
		ctx,
		`SELECT id FROM messages WHERE ending IS NOT NULL AND expires_at > ? AND id > ? ORDER BY id LIMIT ?`,
		ms(now),
		after,
		limit,
	)
}

func (s *Store) PurgeLeases(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.purge(
		ctx,
		`DELETE FROM leases WHERE row_id IN (SELECT row_id FROM leases WHERE lease_expires_at <= ? LIMIT ?)`,
		now,
		limit,
	)
}

func (s *Store) PurgeTombstones(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.purge(
		ctx,
		`DELETE FROM tombstones WHERE id IN (SELECT id FROM tombstones WHERE purge_at <= ? LIMIT ?)`,
		now,
		limit,
	)
}

func (s *Store) RatePurge(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.purge(
		ctx,
		`DELETE FROM rate_counters WHERE (key, window_start) IN (SELECT key, window_start FROM rate_counters WHERE purge_at <= ? LIMIT ?)`,
		now,
		limit,
	)
}

func (s *Store) purge(ctx context.Context, query string, now time.Time, limit int) (int, error) {
	res, err := s.w.ExecContext(ctx, query, ms(now), limit)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *Store) Checkpoint(ctx context.Context) (err error) {
	conn, err := s.w.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA busy_timeout = %d`, checkpointMS)); err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	defer func() {
		if _, rerr := conn.ExecContext(
			context.WithoutCancel(ctx),
			fmt.Sprintf(`PRAGMA busy_timeout = %d`, busyMS),
		); rerr != nil {
			discarded := conn.Raw(func(any) error { return sqldriver.ErrBadConn })
			err = errors.Join(err, fmt.Errorf("sqlite: wal checkpoint: %w", rerr), ignoreBadConn(discarded))
		}
	}()
	var blocked, frames, moved int
	if err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&blocked, &frames, &moved); err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	if blocked != 0 {
		return ErrCheckpointBusy
	}
	return nil
}

func ignoreBadConn(err error) error {
	if errors.Is(err, sqldriver.ErrBadConn) {
		return nil
	}
	return err
}

var ErrCheckpointBusy = errors.New("sqlite: the WAL checkpoint could not finish because a reader held the log")

const leaseInsertAttempts = 4

func randomLeaseRowID() int64 {
	var b [8]byte
	for {
		crand.Read(b[:])
		if id := int64(binary.BigEndian.Uint64(b[:]) >> 1); id >= minLeaseRowID {
			return id
		}
	}
}

func insertLease(ctx context.Context, tx *sql.Tx, id string, p store.ClaimParams, expires time.Time) (int64, error) {
	for range leaseInsertAttempts {
		rowID := randomLeaseRowID()
		res, err := tx.ExecContext(
			ctx,
			`INSERT INTO leases (row_id, id, lease_mac, pepper_id, lease_expires_at, downloads) VALUES (?, ?, ?, ?, ?, 0) ON CONFLICT(row_id) DO NOTHING`,
			rowID,
			id,
			p.LeaseMAC,
			p.LeasePepperID,
			ms(expires),
		)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err != nil || n == 1 {
			return rowID, err
		}
	}
	return 0, errNoLeaseRowID
}

func (s *Store) scanIDs(ctx context.Context, query string, args []any, fn func(id string)) error {
	rows, err := s.r.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		fn(id)
	}
	return rows.Err()
}

func (s *Store) ids(ctx context.Context, query string, args ...any) ([]string, error) {
	var out []string
	err := s.scanIDs(ctx, query, args, func(id string) { out = append(out, id) })
	return out, err
}

func (s *Store) KnownBlobRefs(ctx context.Context) (map[string]struct{}, error) {
	refs := map[string]struct{}{}
	if err := s.scanIDs(
		ctx,
		`SELECT blob_ref FROM messages WHERE blob_ref IS NOT NULL`,
		nil,
		func(ref string) { refs[ref] = struct{}{} },
	); err != nil {
		return nil, err
	}
	return refs, nil
}
