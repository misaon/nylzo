package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"time"

	_ "modernc.org/sqlite"

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
	day_fail_count INTEGER NOT NULL DEFAULT 0,
	day_fail_date TEXT NOT NULL DEFAULT '',
	fail_count_total INTEGER NOT NULL DEFAULT 0,
	first_opened_at INTEGER
);
CREATE INDEX IF NOT EXISTS messages_expires_at ON messages(expires_at);
CREATE INDEX IF NOT EXISTS messages_release ON messages(opens_remaining, blob_ref);
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
CREATE TABLE IF NOT EXISTS passphrase_attempts (
	id TEXT NOT NULL,
	bucket BLOB NOT NULL,
	fail_count INTEGER NOT NULL,
	next_attempt_at INTEGER NOT NULL,
	purge_at INTEGER NOT NULL,
	PRIMARY KEY (id, bucket)
);
CREATE INDEX IF NOT EXISTS passphrase_attempts_purge_at ON passphrase_attempts(purge_at);
CREATE TABLE IF NOT EXISTS tombstones (
	id TEXT PRIMARY KEY,
	pepper_id INTEGER NOT NULL,
	read_mac BLOB NOT NULL,
	manage_mac BLOB NOT NULL,
	state TEXT NOT NULL,
	first_opened_at INTEGER,
	ended_at INTEGER NOT NULL,
	purge_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS tombstones_purge_at ON tombstones(purge_at);
CREATE TABLE IF NOT EXISTS rate_counters (
	key TEXT NOT NULL,
	window_start INTEGER NOT NULL,
	count INTEGER NOT NULL,
	bytes INTEGER NOT NULL,
	purge_at INTEGER NOT NULL,
	PRIMARY KEY (key, window_start)
);
CREATE INDEX IF NOT EXISTS rate_counters_purge_at ON rate_counters(purge_at);
`

type Store struct {
	w *sql.DB
	r *sql.DB
}

func dsn(path string, write bool) string {
	q := url.Values{}
	if write {
		q.Set("_txlock", "immediate")
	}
	q.Set("_busy_timeout", "5000")
	q.Set("_journal_mode", "WAL")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	return "file:" + path + "?" + q.Encode()
}

func Open(path string) (*Store, error) {
	w, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if _, err := w.Exec(schema); err != nil {
		w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		w.Close()
		return nil, err
	}
	return &Store{w: w, r: r}, nil
}

func (s *Store) Close() error {
	rerr := s.r.Close()
	werr := s.w.Close()
	if werr != nil {
		return werr
	}
	return rerr
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
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) Create(ctx context.Context, m *store.Message) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT id FROM messages WHERE id = ? UNION ALL SELECT id FROM tombstones WHERE id = ?)`, m.ID, m.ID).Scan(&exists)
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
		_, err = tx.ExecContext(ctx, `INSERT INTO messages (id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, verifier_mac, size, blob_ref)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Version, m.KDF, m.Flags, string(store.StateAvailable), ms(m.CreatedAt), ms(m.ExpiresAt), m.TTLSeconds, m.OpensMax, m.OpensMax, m.PepperID, m.ReadMAC, m.ManageMAC, verifier, m.Size, blobRef)
		return err
	})
}

const recordQuery = `SELECT 'm' AS kind, id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, verifier_mac, size, blob_ref, day_fail_count, day_fail_date, fail_count_total, first_opened_at, NULL AS ended_at, NULL AS purge_at FROM messages WHERE id = ?
UNION ALL
SELECT 't', id, 0, 0, 0, state, 0, 0, 0, 0, 0, pepper_id, read_mac, manage_mac, NULL, 0, NULL, 0, '', 0, first_opened_at, ended_at, purge_at FROM tombstones WHERE id = ?`

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func scanRecord(ctx context.Context, q querier, id string) (store.Record, error) {
	var (
		kind                                    string
		m                                       store.Message
		state                                   string
		createdAt, expiresAt                    int64
		verifier                                []byte
		blobRef                                 sql.NullString
		firstOpened, endedAt, purgeAt           sql.NullInt64
		version, kdf, flags, opensMax, opensRem int
		pepperID, ttl, size, dayFail, total     int64
		dayFailDate                             string
	)
	row := q.QueryRowContext(ctx, recordQuery, id, id)
	err := row.Scan(&kind, &m.ID, &version, &kdf, &flags, &state, &createdAt, &expiresAt, &ttl, &opensMax, &opensRem, &pepperID, &m.ReadMAC, &m.ManageMAC, &verifier, &size, &blobRef, &dayFail, &dayFailDate, &total, &firstOpened, &endedAt, &purgeAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Record{}, store.ErrNotFound
	}
	if err != nil {
		return store.Record{}, err
	}
	if kind == "t" {
		return store.Record{Tombstone: &store.Tombstone{ID: m.ID, PepperID: byte(pepperID), ReadMAC: m.ReadMAC, ManageMAC: m.ManageMAC, State: store.State(state), FirstOpenedAt: optTime(firstOpened), EndedAt: fromMS(endedAt.Int64), PurgeAt: fromMS(purgeAt.Int64)}}, nil
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
	m.DayFailCount = int(dayFail)
	m.DayFailDate = dayFailDate
	m.FailCountTotal = int(total)
	m.FirstOpenedAt = optTime(firstOpened)
	return store.Record{Message: &m}, nil
}

func (s *Store) Get(ctx context.Context, id string) (store.Record, error) {
	return scanRecord(ctx, s.r, id)
}

func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func untilMidnight(now time.Time) time.Duration {
	u := now.UTC()
	next := time.Date(u.Year(), u.Month(), u.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(u)
}

func backoff(failCount int) time.Duration {
	if failCount <= store.FreeAttempts {
		return 0
	}
	exp := failCount - store.FreeAttempts
	if exp > 20 {
		return store.MaxBackoff
	}
	d := time.Duration(math.Pow(2, float64(exp))) * time.Second
	if d > store.MaxBackoff {
		return store.MaxBackoff
	}
	return d
}

func (s *Store) Claim(ctx context.Context, p store.ClaimParams) (store.ClaimResult, error) {
	var result store.ClaimResult
	var passErr *store.PassphraseError
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rec, err := scanRecord(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if rec.Tombstone != nil {
			return &store.GoneError{State: rec.Tombstone.State}
		}
		m := rec.Message
		if m.State != store.StateAvailable {
			return &store.GoneError{State: m.State}
		}
		if !p.Now.Before(m.ExpiresAt) {
			return &store.GoneError{State: store.StateExpired}
		}
		if m.PassphraseRequired() {
			today := dayKey(p.Now)
			dayCount := 0
			if m.DayFailDate == today {
				dayCount = m.DayFailCount
			}
			if dayCount >= store.DailyFailCap {
				passErr = &store.PassphraseError{RetryAfter: untilMidnight(p.Now)}
				return nil
			}
			var failCount int
			var nextAttempt int64
			err := tx.QueryRowContext(ctx, `SELECT fail_count, next_attempt_at FROM passphrase_attempts WHERE id = ? AND bucket = ?`, m.ID, p.Bucket).Scan(&failCount, &nextAttempt)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if nextAttempt > ms(p.Now) {
				passErr = &store.PassphraseError{RetryAfter: fromMS(nextAttempt).Sub(p.Now)}
				return nil
			}
			ok := p.VerifierSent && p.VerifyMAC(m.PepperID, m.VerifierMAC)
			if !ok {
				failCount++
				dayCount++
				total := m.FailCountTotal + 1
				wait := backoff(failCount)
				next := p.Now.Add(wait)
				purge := m.ExpiresAt.Add(store.TombstoneTTL)
				if _, err := tx.ExecContext(ctx, `UPDATE messages SET day_fail_count = ?, day_fail_date = ?, fail_count_total = ? WHERE id = ?`, dayCount, today, total, m.ID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO passphrase_attempts (id, bucket, fail_count, next_attempt_at, purge_at) VALUES (?, ?, ?, ?, ?)
					ON CONFLICT(id, bucket) DO UPDATE SET fail_count = excluded.fail_count, next_attempt_at = excluded.next_attempt_at, purge_at = excluded.purge_at`,
					m.ID, p.Bucket, failCount, ms(next), ms(purge)); err != nil {
					return err
				}
				left := store.FreeAttempts - failCount
				if left < 0 {
					left = 0
				}
				passErr = &store.PassphraseError{RetryAfter: wait, AttemptsLeft: left}
				return nil
			}
		}
		m.OpensRemaining--
		if m.OpensRemaining == 0 {
			m.State = store.StateOpened
		}
		if m.FirstOpenedAt == nil {
			t := p.Now.UTC().Truncate(time.Minute)
			m.FirstOpenedAt = &t
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET opens_remaining = ?, state = ?, first_opened_at = ? WHERE id = ?`, m.OpensRemaining, string(m.State), ms(*m.FirstOpenedAt), m.ID); err != nil {
			return err
		}
		expires := p.Now.Add(store.LeaseDuration)
		res, err := tx.ExecContext(ctx, `INSERT INTO leases (id, lease_mac, pepper_id, lease_expires_at, downloads) VALUES (?, ?, ?, ?, 0)`, m.ID, p.LeaseMAC, p.LeasePepperID, ms(expires))
		if err != nil {
			return err
		}
		rowID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		result = store.ClaimResult{Message: m, Lease: store.Lease{RowID: rowID, ID: m.ID, MAC: p.LeaseMAC, PepperID: p.LeasePepperID, ExpiresAt: expires}}
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
	rows, err := s.r.QueryContext(ctx, `SELECT row_id, id, lease_mac, pepper_id, lease_expires_at, downloads FROM leases WHERE id = ? ORDER BY row_id`, id)
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

func (s *Store) RecordDownload(ctx context.Context, leaseRowID int64, now time.Time) (int, error) {
	var downloads int
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var expires int64
		err := tx.QueryRowContext(ctx, `SELECT lease_expires_at, downloads FROM leases WHERE row_id = ?`, leaseRowID).Scan(&expires, &downloads)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && expires <= ms(now)) {
			return store.ErrLeaseInvalid
		}
		if err != nil {
			return err
		}
		if downloads >= store.MaxLeaseDownload {
			return store.ErrLeaseLimit
		}
		downloads++
		_, err = tx.ExecContext(ctx, `UPDATE leases SET downloads = ? WHERE row_id = ?`, downloads, leaseRowID)
		return err
	})
	return downloads, err
}

func (s *Store) Ack(ctx context.Context, leaseRowID int64) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM leases WHERE row_id = ?`, leaseRowID)
	return err
}

func (s *Store) ReleaseIfDone(ctx context.Context, id string, now time.Time) (string, error) {
	var ref string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var opensRemaining int
		var blobRef sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT opens_remaining, blob_ref FROM messages WHERE id = ?`, id).Scan(&opensRemaining, &blobRef)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if opensRemaining != 0 || !blobRef.Valid || blobRef.String == "" {
			return nil
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM leases WHERE id = ? AND lease_expires_at > ?`, id, ms(now)).Scan(&active); err != nil {
			return err
		}
		if active == 0 {
			ref = blobRef.String
		}
		return nil
	})
	return ref, err
}

func (s *Store) ClearBlob(ctx context.Context, id string, blobRef string, now time.Time) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		rec, err := scanRecord(ctx, tx, id)
		if errors.Is(err, store.ErrNotFound) || (err == nil && (rec.Message == nil || rec.Message.BlobRef != blobRef)) {
			return nil
		}
		if err != nil {
			return err
		}
		return endInTx(ctx, tx, rec.Message, store.StateOpened, now)
	})
}

func (s *Store) End(ctx context.Context, id string, state store.State, now time.Time) (string, error) {
	var ref string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rec, err := scanRecord(ctx, tx, id)
		if err != nil {
			return err
		}
		if rec.Message == nil {
			return store.ErrNotFound
		}
		ref = rec.Message.BlobRef
		return endInTx(ctx, tx, rec.Message, state, now)
	})
	return ref, err
}

func endInTx(ctx context.Context, tx *sql.Tx, m *store.Message, state store.State, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, m.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM passphrase_attempts WHERE id = ?`, m.ID); err != nil {
		return err
	}
	var firstOpened any
	if m.FirstOpenedAt != nil {
		firstOpened = ms(*m.FirstOpenedAt)
	}
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO tombstones (id, pepper_id, read_mac, manage_mac, state, first_opened_at, ended_at, purge_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.PepperID, m.ReadMAC, m.ManageMAC, string(state), firstOpened, ms(now), ms(now.Add(store.TombstoneTTL)))
	return err
}

func (s *Store) RateHit(ctx context.Context, hits []store.RateHit) ([]store.RateCount, error) {
	out := make([]store.RateCount, 0, len(hits))
	err := s.tx(ctx, func(tx *sql.Tx) error {
		for _, h := range hits {
			var c store.RateCount
			err := tx.QueryRowContext(ctx, `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES (?, ?, 1, ?, ?)
				ON CONFLICT(key, window_start) DO UPDATE SET count = count + 1, bytes = bytes + excluded.bytes RETURNING count, bytes`,
				h.Key, ms(h.WindowStart), h.Bytes, ms(h.PurgeAt)).Scan(&c.Count, &c.Bytes)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return nil
	})
	return out, err
}

func (s *Store) RateGet(ctx context.Context, key string, windowStart time.Time) (store.RateCount, error) {
	var c store.RateCount
	err := s.r.QueryRowContext(ctx, `SELECT count, bytes FROM rate_counters WHERE key = ? AND window_start = ?`, key, ms(windowStart)).Scan(&c.Count, &c.Bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RateCount{}, nil
	}
	return c, err
}

func (s *Store) Sweep(ctx context.Context, now time.Time, deleteBlob func(ref string) error) (store.SweepReport, error) {
	var r store.SweepReport
	for {
		ids, err := s.queryIDs(ctx, `SELECT id FROM messages WHERE expires_at <= ? LIMIT ?`, ms(now), store.SweepBatch)
		if err != nil {
			return r, err
		}
		for _, id := range ids {
			ref, err := s.End(ctx, id, store.StateExpired, now)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return r, err
			}
			if err == nil {
				r.Expired++
			}
			if ref != "" {
				if err := deleteBlob(ref); err != nil {
					return r, err
				}
			}
		}
		if len(ids) < store.SweepBatch {
			break
		}
	}
	n, err := s.purge(ctx, "leases", "row_id", "lease_expires_at", now)
	if err != nil {
		return r, err
	}
	r.ExpiredLeases = n
	ids, err := s.queryIDs(ctx, `SELECT id FROM messages WHERE opens_remaining = 0 AND blob_ref IS NOT NULL`)
	if err != nil {
		return r, err
	}
	for _, id := range ids {
		ref, err := s.ReleaseIfDone(ctx, id, now)
		if err != nil {
			return r, err
		}
		if ref == "" {
			continue
		}
		if err := deleteBlob(ref); err != nil {
			return r, err
		}
		if err := s.ClearBlob(ctx, id, ref, now); err != nil {
			return r, err
		}
		r.BlobsReleased++
	}
	if r.Tombstones, err = s.purge(ctx, "tombstones", "rowid", "purge_at", now); err != nil {
		return r, err
	}
	if r.Counters, err = s.purge(ctx, "rate_counters", "rowid", "purge_at", now); err != nil {
		return r, err
	}
	if r.Attempts, err = s.purge(ctx, "passphrase_attempts", "rowid", "purge_at", now); err != nil {
		return r, err
	}
	return r, nil
}

func (s *Store) queryIDs(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) purge(ctx context.Context, table, key, column string, now time.Time) (int, error) {
	total := 0
	for {
		res, err := s.w.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s IN (SELECT %s FROM %s WHERE %s <= ? LIMIT %d)`, table, key, key, table, column, store.SweepBatch), ms(now))
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += int(n)
		if n < store.SweepBatch {
			return total, nil
		}
	}
}

func (s *Store) KnownBlobRefs(ctx context.Context) (map[string]struct{}, error) {
	refs, err := s.queryIDs(ctx, `SELECT blob_ref FROM messages WHERE blob_ref IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		out[r] = struct{}{}
	}
	return out, nil
}
