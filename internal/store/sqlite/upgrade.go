package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// releasedSchema is the schema of nylzo 0.1 and 0.2.
const releasedSchema = 5

var errUpgradeRaced = errors.New("sqlite: another process changed the schema during the upgrade")

// The version changes last, after the blobs, the rows and the log, so an interrupted upgrade runs again in full.
func UpgradeFromReleased(ctx context.Context, path string, eraseBlobs func() error) (bool, error) {
	writer, err := openPersistent(path, true)
	if err != nil {
		return false, err
	}
	writer.SetMaxOpenConns(1)
	version, err := userVersion(ctx, writer)
	if err != nil || version != releasedSchema {
		return false, errors.Join(err, writer.Close())
	}
	if err = eraseBlobs(); err != nil {
		return false, errors.Join(fmt.Errorf("sqlite: upgrade: erase the stored blobs: %w", err), writer.Close())
	}
	err = deleteReleasedMessages(ctx, writer)
	if err = errors.Join(err, writer.Close()); err != nil {
		return false, fmt.Errorf("sqlite: upgrade: %w", err)
	}
	return true, finishUpgrade(ctx, path)
}

// secure_delete zeroes the pages that the deleted rows free.
func deleteReleasedMessages(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	version, err := userVersion(ctx, tx)
	if err != nil {
		return err
	}
	if version != releasedSchema {
		return errUpgradeRaced
	}
	for _, statement := range []string{
		`DROP TRIGGER IF EXISTS rate_counters_guard_insert`,
		`DROP TRIGGER IF EXISTS rate_counters_guard_update`,
		`DELETE FROM messages`,
		`DELETE FROM rate_counters`,
		schema,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// The log still holds the deleted rows until the checkpoint overwrites it.
func finishUpgrade(ctx context.Context, path string) error {
	opened, err := openWith(ctx, path, func(ctx context.Context, database *sql.DB) error {
		version, versionErr := userVersion(ctx, database)
		if versionErr == nil && version != releasedSchema {
			versionErr = errUpgradeRaced
		}
		return versionErr
	})
	if err != nil {
		return fmt.Errorf("sqlite: upgrade: %w", err)
	}
	err = opened.checkpointWithRetries(ctx)
	if err == nil {
		_, err = opened.writer.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	}
	if err = errors.Join(err, opened.Close()); err != nil {
		return fmt.Errorf("sqlite: upgrade: %w", err)
	}
	return nil
}

func (s *Store) checkpointWithRetries(ctx context.Context) error {
	var err error
	for range scrubRetries {
		if err = s.Checkpoint(ctx); !errors.Is(err, ErrCheckpointBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(scrubRetryDelay):
		}
	}
	return err
}
