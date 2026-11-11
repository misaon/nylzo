package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const (
	hitQuery = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES (?, ?, 1, 0, ?)
		ON CONFLICT(key, window_start) DO UPDATE SET count = count + 1 RETURNING count`
	reserveQuery = `INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES (?, ?, 1, 0, ?)
		ON CONFLICT(key, window_start) DO UPDATE SET count = count + 1 WHERE count < ? RETURNING count`
	releaseQuery = `UPDATE rate_counters SET count = count - 1 WHERE key = ? AND window_start = ? AND count > 0`
	counterQuery = `SELECT count FROM rate_counters WHERE key = ? AND window_start = ?`
	purgeQuery   = `DELETE FROM rate_counters WHERE (key, window_start) IN
		(SELECT key, window_start FROM rate_counters WHERE purge_at <= ? LIMIT ?)`
)

func (s *Store) RateHit(ctx context.Context, hits []store.RateHit) ([]store.RateCount, error) {
	counts := make([]store.RateCount, 0, len(hits))
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		statement := tx.StmtContext(ctx, s.statements.hitCounter)
		for i := range hits {
			count, err := countHit(ctx, statement, &hits[i])
			if err != nil {
				return err
			}
			counts = append(counts, count)
		}
		return nil
	})
	return counts, err
}

func (s *Store) RateReserve(ctx context.Context, hit store.RateHit) (int64, error) {
	var count int64
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		count, err = reserveHit(ctx, tx.StmtContext(ctx, s.statements.reserveCounter), &hit)
		return err
	})
	return count, err
}

func (s *Store) RateRelease(ctx context.Context, key string, windowStart time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		_, err := execIn(ctx, tx, s.statements.releaseCounter, key, milliseconds(windowStart))
		return err
	})
}

// RateCopy sets an existing counter to the count of another one in the same window, or to 0 when that has none.
func (s *Store) RateCopy(ctx context.Context, sourceKey, targetKey string, windowStart time.Time) error {
	window := milliseconds(windowStart)
	return s.transaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`UPDATE rate_counters SET count = COALESCE(
				(SELECT count FROM rate_counters WHERE key = ? AND window_start = ?), 0
			) WHERE key = ? AND window_start = ?`,
			sourceKey,
			window,
			targetKey,
			window,
		)
		return err
	})
}

func (s *Store) RateGet(ctx context.Context, key string, windowStart time.Time) (store.RateCount, error) {
	var count store.RateCount
	err := s.statements.loadCounter.QueryRowContext(ctx, key, milliseconds(windowStart)).Scan(&count.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RateCount{}, nil
	}
	return count, err
}

func (s *Store) RatePurge(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.purge(ctx, purgeQuery, now, limit)
}

func countHit(ctx context.Context, statement *sql.Stmt, hit *store.RateHit) (store.RateCount, error) {
	var count store.RateCount
	err := statement.QueryRowContext(ctx, hit.Key, milliseconds(hit.WindowStart), milliseconds(hit.PurgeAt)).
		Scan(&count.Count)
	return count, err
}

func reserveHit(ctx context.Context, statement *sql.Stmt, hit *store.RateHit) (int64, error) {
	var count int64
	err := statement.QueryRowContext(ctx, hit.Key, milliseconds(hit.WindowStart), milliseconds(hit.PurgeAt), hit.Limit).
		Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}
