package adminstore

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Resolution is the length of a period in seconds.
type Resolution int64

const (
	Hourly Resolution = 3600
	Daily  Resolution = 86400
)

// Start gives the start of the period of this resolution that holds the moment, in UTC.
func (r Resolution) Start(moment time.Time) time.Time {
	seconds := moment.Unix()
	return time.Unix(seconds-seconds%int64(r), 0).UTC()
}

// Count is how often a metric happened in the period that starts at Start.
type Count struct {
	Start  time.Time
	Metric string
	Value  int64
	// DailyOnly keeps the count out of the hours.
	DailyOnly bool
}

// AddCounts adds each count to its hour and to its day, so a chart of either resolution reads one row per point.
func (s *Store) AddCounts(ctx context.Context, counts []Count) error {
	if len(counts) == 0 {
		return nil
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		statement, err := tx.PrepareContext(
			ctx,
			`INSERT INTO counts (resolution, period_start, metric, value) VALUES (?, ?, ?, ?)
				ON CONFLICT(resolution, period_start, metric) DO UPDATE SET value = value + excluded.value`,
		)
		if err != nil {
			return err
		}
		defer statement.Close()
		for _, count := range counts {
			if addErr := addCount(ctx, statement, &count); addErr != nil {
				return addErr
			}
		}
		return nil
	})
}

func addCount(ctx context.Context, statement *sql.Stmt, count *Count) error {
	for _, resolution := range []Resolution{Hourly, Daily} {
		if count.DailyOnly && resolution == Hourly {
			continue
		}
		start := resolution.Start(count.Start).Unix()
		if _, err := statement.ExecContext(ctx, int64(resolution), start, count.Metric, count.Value); err != nil {
			return err
		}
	}
	return nil
}

// Counts reads the periods of a resolution that start in [from, until).
func (s *Store) Counts(ctx context.Context, resolution Resolution, from, until time.Time) ([]Count, error) {
	rows, err := s.database.QueryContext(
		ctx,
		`SELECT period_start, metric, value FROM counts
			WHERE resolution = ? AND period_start >= ? AND period_start < ? ORDER BY period_start, metric`,
		int64(resolution),
		from.Unix(),
		until.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var counts []Count
	for rows.Next() {
		var (
			count Count
			start int64
		)
		if scanErr := rows.Scan(&start, &count.Metric, &count.Value); scanErr != nil {
			return nil, scanErr
		}
		count.Start = time.Unix(start, 0).UTC()
		counts = append(counts, count)
	}
	return counts, rows.Err()
}

// ForgetCounts drops the hours that started before hoursFrom and the days that started before daysFrom.
func (s *Store) ForgetCounts(ctx context.Context, hoursFrom, daysFrom time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`DELETE FROM counts WHERE (resolution = ? AND period_start < ?) OR (resolution = ? AND period_start < ?)`,
			int64(Hourly),
			hoursFrom.Unix(),
			int64(Daily),
			daysFrom.Unix(),
		)
		return err
	})
}

// Carry sets aside the counts of periods that still run when the server stops. They are no rows of a chart: the
// next start takes them back, so the disk holds them only while the server is down.
func (s *Store) Carry(ctx context.Context, counts []Count) error {
	if len(counts) == 0 {
		return nil
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		for _, count := range counts {
			if _, err := tx.ExecContext(
				ctx,
				`INSERT INTO carried (period_start, metric, value) VALUES (?, ?, ?)
					ON CONFLICT(period_start, metric) DO UPDATE SET value = value + excluded.value`,
				count.Start.Unix(),
				count.Metric,
				count.Value,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// TakeCarried hands the carried counts back once and removes them.
func (s *Store) TakeCarried(ctx context.Context) ([]Count, error) {
	var counts []Count
	err := s.write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT period_start, metric, value FROM carried`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				count Count
				start int64
			)
			if scanErr := rows.Scan(&start, &count.Metric, &count.Value); scanErr != nil {
				return scanErr
			}
			count.Start = time.Unix(start, 0).UTC()
			counts = append(counts, count)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return rowsErr
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM carried`)
		return err
	})
	if err != nil && !errors.Is(err, ErrJournalNotErased) {
		return nil, err
	}
	return counts, err
}
