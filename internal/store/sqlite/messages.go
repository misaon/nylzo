package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const recordQuery = `SELECT 'm' AS kind, id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, verifier_mac, size, blob_ref, fail_count_total, next_attempt_at, first_opened_at, NULL AS ended_at, NULL AS purge_at, ending FROM messages WHERE id = ?
UNION ALL
SELECT 't', id, 0, 0, 0, state, 0, 0, 0, 0, 0, pepper_id, read_mac, manage_mac, NULL, 0, NULL, 0, 0, first_opened_at, ended_at, purge_at, NULL FROM tombstones WHERE id = ?`

var errCorruptRow = errors.New("sqlite: a row holds a value its column cannot have")

func (s *Store) Create(ctx context.Context, message *store.Message, counters ...store.RateHit) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := ensureNewID(ctx, tx, message.ID); err != nil {
			return err
		}
		if err := insertMessage(ctx, tx, message); err != nil {
			return err
		}
		if len(counters) == 0 {
			return nil
		}
		statement := tx.StmtContext(ctx, s.statements.hitCounter)
		for i := range counters {
			if _, err := countHit(ctx, statement, &counters[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Get(ctx context.Context, id string) (store.Record, error) {
	return scanRecord(s.statements.getRecord.QueryRowContext(ctx, id, id))
}

func (s *Store) recordIn(ctx context.Context, tx *sql.Tx, id string) (store.Record, error) {
	statement := tx.StmtContext(ctx, s.statements.lockedRecord)
	defer statement.Close()
	return scanRecord(statement.QueryRowContext(ctx, id, id))
}

func ensureNewID(ctx context.Context, tx *sql.Tx, id string) error {
	var exists int
	err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM (SELECT id FROM messages WHERE id = ? UNION ALL SELECT id FROM tombstones WHERE id = ?)`,
		id,
		id,
	).Scan(&exists)
	if err != nil {
		return err
	}
	if exists > 0 {
		return store.ErrConflict
	}
	return nil
}

func insertMessage(ctx context.Context, tx *sql.Tx, message *store.Message) error {
	var verifierMAC, blobRef any
	if len(message.VerifierMAC) > 0 {
		verifierMAC = message.VerifierMAC
	}
	if message.BlobRef != "" {
		blobRef = message.BlobRef
	}
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO messages (id, version, kdf_id, flags, state, created_at, expires_at, ttl_s, opens_max, opens_remaining, pepper_id, read_mac, manage_mac, verifier_mac, size, blob_ref)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID,
		message.Version,
		message.KDF,
		message.Flags,
		string(store.StateAvailable),
		ms(message.CreatedAt),
		ms(message.ExpiresAt),
		message.TTLSeconds,
		message.OpensMax,
		message.OpensMax,
		message.PepperID,
		message.ReadMAC,
		message.ManageMAC,
		verifierMAC,
		message.Size,
		blobRef,
	)
	return err
}

type recordRow struct {
	kind, id, state                               string
	version, kdf, flags, ttl, pepperID            int64
	opensMax, opensRemaining                      int64
	createdAt, expiresAt, size, failCount, nextAt int64
	readMAC, manageMAC, verifierMAC               []byte
	blobRef, ending                               sql.NullString
	firstOpenedAt, endedAt, purgeAt               sql.NullInt64
}

func scanRecord(row *sql.Row) (store.Record, error) {
	var raw recordRow
	err := row.Scan(
		&raw.kind,
		&raw.id,
		&raw.version,
		&raw.kdf,
		&raw.flags,
		&raw.state,
		&raw.createdAt,
		&raw.expiresAt,
		&raw.ttl,
		&raw.opensMax,
		&raw.opensRemaining,
		&raw.pepperID,
		&raw.readMAC,
		&raw.manageMAC,
		&raw.verifierMAC,
		&raw.size,
		&raw.blobRef,
		&raw.failCount,
		&raw.nextAt,
		&raw.firstOpenedAt,
		&raw.endedAt,
		&raw.purgeAt,
		&raw.ending,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Record{}, store.ErrNotFound
	}
	if err != nil {
		return store.Record{}, err
	}
	if raw.kind == "t" {
		return raw.tombstone()
	}
	return raw.message()
}

func (raw *recordRow) tombstone() (store.Record, error) {
	tombstone := &store.Tombstone{
		ID:            raw.id,
		ReadMAC:       raw.readMAC,
		ManageMAC:     raw.manageMAC,
		State:         store.State(raw.state),
		FirstOpenedAt: optionalTime(raw.firstOpenedAt),
		EndedAt:       fromMS(raw.endedAt.Int64).Truncate(time.Minute),
		PurgeAt:       fromMS(raw.purgeAt.Int64),
	}
	if err := narrow(&tombstone.PepperID, "pepper_id", raw.pepperID); err != nil {
		return store.Record{}, err
	}
	return store.Record{Tombstone: tombstone}, nil
}

func (raw *recordRow) message() (store.Record, error) {
	if raw.opensMax < 1 || raw.opensMax > math.MaxUint16 || raw.opensRemaining < 0 ||
		raw.opensRemaining > raw.opensMax {
		return store.Record{}, fmt.Errorf("%w: opens %d of %d", errCorruptRow, raw.opensRemaining, raw.opensMax)
	}
	message := &store.Message{
		ID:             raw.id,
		State:          store.State(raw.state),
		CreatedAt:      fromMS(raw.createdAt),
		ExpiresAt:      fromMS(raw.expiresAt),
		ReadMAC:        raw.readMAC,
		ManageMAC:      raw.manageMAC,
		VerifierMAC:    raw.verifierMAC,
		Size:           raw.size,
		BlobRef:        raw.blobRef.String,
		FailCountTotal: int(raw.failCount),
		FirstOpenedAt:  optionalTime(raw.firstOpenedAt),
		Ending:         store.State(raw.ending.String),
	}
	err := errors.Join(
		narrow(&message.Version, "version", raw.version),
		narrow(&message.KDF, "kdf_id", raw.kdf),
		narrow(&message.Flags, "flags", raw.flags),
		narrow(&message.TTLSeconds, "ttl_s", raw.ttl),
		narrow(&message.OpensMax, "opens_max", raw.opensMax),
		narrow(&message.OpensRemaining, "opens_remaining", raw.opensRemaining),
		narrow(&message.PepperID, "pepper_id", raw.pepperID),
	)
	if err != nil {
		return store.Record{}, err
	}
	if raw.nextAt > 0 {
		message.NextAttemptAt = fromMS(raw.nextAt)
	}
	return store.Record{Message: message}, nil
}

func narrow[T uint8 | uint16 | uint32](target *T, column string, value int64) error {
	if value < 0 || uint64(value) > uint64(^T(0)) {
		return fmt.Errorf("%w: %s %d", errCorruptRow, column, value)
	}
	*target = T(value)
	return nil
}

func ms(moment time.Time) int64 {
	return moment.UnixMilli()
}

func fromMS(milliseconds int64) time.Time {
	return time.UnixMilli(milliseconds).UTC()
}

func optionalTime(milliseconds sql.NullInt64) *time.Time {
	if !milliseconds.Valid {
		return nil
	}
	return new(fromMS(milliseconds.Int64))
}
