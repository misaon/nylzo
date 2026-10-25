package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

//nolint:gocritic // unnamedResult: store.Messages declares this signature; see the cross-area list.
func (s *Store) MarkEnding(
	ctx context.Context,
	id string,
	state store.State,
	now time.Time,
) (store.State, string, bool, error) {
	var ending store.State
	var ref string
	recorded := false
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		record, err := s.recordIn(ctx, tx, id)
		if err != nil {
			return err
		}
		message := record.Message
		if message == nil {
			return store.ErrNotFound
		}
		ref = message.BlobRef
		ending = chooseEnding(message, state, now)
		if message.Ending != "" || !now.Before(message.ExpiresAt) {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE messages SET ending = ? WHERE id = ?`, string(ending), id); err != nil {
			return err
		}
		recorded = true
		return nil
	})
	return ending, ref, recorded && err == nil, err
}

func (s *Store) End(ctx context.Context, id string, state store.State, now time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		record, err := s.recordIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if record.Message == nil {
			return store.ErrNotFound
		}
		return endInTx(ctx, tx, record.Message, endingOr(record.Message, state), now)
	})
}

func (s *Store) EndExpired(ctx context.Context, ids []string, now time.Time) (int, error) {
	var ended int
	var errs []error
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		ended, errs = 0, nil
		for _, id := range ids {
			done, err := s.endExpiredInTx(ctx, tx, id, now)
			if errors.Is(err, errCorruptRow) {
				errs = append(errs, fmt.Errorf("sqlite: end expired: %w", err))
				continue
			}
			if err != nil {
				return err
			}
			if done {
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
	record, err := s.recordIn(ctx, tx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && record.Message == nil) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	message := record.Message
	if now.Before(message.ExpiresAt) {
		return false, nil
	}
	ending := store.StateExpired
	if message.OpensRemaining == 0 {
		ending = store.StateOpened
	}
	return true, endInTx(ctx, tx, message, endingOr(message, ending), now)
}

func chooseEnding(message *store.Message, requested store.State, now time.Time) store.State {
	expired := !now.Before(message.ExpiresAt)
	switch {
	case message.Ending != "":
		return message.Ending
	case message.OpensRemaining == 0 && (expired || requested == store.StateExpired):
		return store.StateOpened
	case expired:
		return store.StateExpired
	default:
		return requested
	}
}

func endingOr(message *store.Message, state store.State) store.State {
	if message.Ending != "" {
		return message.Ending
	}
	return state
}

func endInTx(ctx context.Context, tx *sql.Tx, message *store.Message, state store.State, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, message.ID); err != nil {
		return err
	}
	var firstOpenedAt any
	if message.FirstOpenedAt != nil {
		firstOpenedAt = milliseconds(*message.FirstOpenedAt)
	}
	ended := now
	if !now.Before(message.ExpiresAt) {
		ended = message.ExpiresAt
	}
	ended = ended.UTC().Truncate(time.Minute)
	_, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO tombstones (id, pepper_id, read_mac, manage_mac, state, first_opened_at, ended_at, purge_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID,
		message.PepperID,
		message.ReadMAC,
		message.ManageMAC,
		string(state),
		firstOpenedAt,
		milliseconds(ended),
		milliseconds(ended.Add(store.TombstoneTTL)),
	)
	return err
}
