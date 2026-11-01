package sqlite

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	checkpointTimeoutMilliseconds = 250
	walHeaderLen                  = 32
	walFrameHeaderLen             = 24
	scrubFillerLen                = 3000
	maxScrubGrowths               = 4
	scrubRetries                  = 5
	scrubRetryDelay               = time.Second
)

var (
	ErrCheckpointBusy = errors.New("sqlite: the WAL checkpoint could not finish because a reader held the log")
	errScrubShort     = errors.New("sqlite: the scrub table did not grow to the size of the log")
)

// Checkpoint copies the log into the database and then overwrites every frame of the log in place (spec §5.2).
func (s *Store) Checkpoint(ctx context.Context) error {
	connection, err := s.writer.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	defer connection.Close()
	if _, err = connection.ExecContext(
		ctx,
		fmt.Sprintf(`PRAGMA busy_timeout = %d`, checkpointTimeoutMilliseconds),
	); err != nil {
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	}
	err = s.scrub(ctx, connection)
	restore := fmt.Sprintf(`PRAGMA busy_timeout = %d`, busyTimeoutMilliseconds)
	if _, restoreErr := connection.ExecContext(context.WithoutCancel(ctx), restore); restoreErr != nil {
		discarded := connection.Raw(func(any) error { return sqldriver.ErrBadConn })
		return errors.Join(err, fmt.Errorf("sqlite: wal checkpoint: %w", restoreErr), ignoreBadConn(discarded))
	}
	return err
}

// scrub rewrites one zero-filled page per frame from the start of the log, over every older version of a row.
func (s *Store) scrub(ctx context.Context, connection *sql.Conn) error {
	for range maxScrubGrowths {
		if err := restartLog(ctx, connection); err != nil {
			return err
		}
		frames, err := s.walFrames()
		if err != nil {
			return fmt.Errorf("sqlite: wal scrub: %w", err)
		}
		var slots int
		if err = connection.QueryRowContext(ctx, `SELECT COUNT(*) FROM wal_scrub`).Scan(&slots); err != nil {
			return fmt.Errorf("sqlite: wal scrub: %w", err)
		}
		if slots >= frames {
			_, err = connection.ExecContext(ctx, `UPDATE wal_scrub SET generation = 1 - generation`)
			return wrapScrub(err)
		}
		if growErr := growScrub(ctx, connection, frames-slots); growErr != nil {
			return growErr
		}
	}
	return errScrubShort
}

// restartLog checkpoints the whole log and waits until no reader uses it, so the next write starts at its first frame.
func restartLog(ctx context.Context, connection *sql.Conn) error {
	var blocked, frames, moved int
	err := connection.QueryRowContext(ctx, `PRAGMA wal_checkpoint(RESTART)`).Scan(&blocked, &frames, &moved)
	switch {
	case err != nil:
		return fmt.Errorf("sqlite: wal checkpoint: %w", err)
	case blocked != 0:
		return ErrCheckpointBusy
	default:
		return nil
	}
}

// growScrub adds slack for the pages the insert itself writes, so the scrub soon covers the log in one pass.
func growScrub(ctx context.Context, connection *sql.Conn, missing int) error {
	_, err := connection.ExecContext(
		ctx,
		`WITH RECURSIVE slot(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM slot WHERE n < ?)
		INSERT INTO wal_scrub (generation, filler) SELECT 0, zeroblob(?) FROM slot`,
		missing+missing/16+4,
		scrubFillerLen,
	)
	return wrapScrub(err)
}

func wrapScrub(err error) error {
	if err != nil {
		return fmt.Errorf("sqlite: wal scrub: %w", err)
	}
	return nil
}

func (s *Store) walFrames() (int, error) {
	info, err := os.Stat(s.path + "-wal")
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	frames := (info.Size() - walHeaderLen + walFrameHeaderLen + int64(s.pageSize) - 1) /
		int64(walFrameHeaderLen+s.pageSize)
	return int(max(frames, 0)), nil
}

func (s *Store) requestScrub() {
	select {
	case s.scrubs <- struct{}{}:
	default:
	}
}

// scrubWhenAsked runs the scrubs that endings ask for, one at a time, and tries a busy one again a few times.
func (s *Store) scrubWhenAsked(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.scrubs:
		}
		for range scrubRetries {
			if err := s.Checkpoint(ctx); !errors.Is(err, ErrCheckpointBusy) {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(scrubRetryDelay):
			}
		}
	}
}

func ignoreBadConn(err error) error {
	if errors.Is(err, sqldriver.ErrBadConn) {
		return nil
	}
	return err
}
