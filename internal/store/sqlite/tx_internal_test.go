package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAPanicInATransactionFreesTheWriter(t *testing.T) {
	s := openTest(t)
	func() {
		defer func() { recover() }()
		s.tx(context.WithoutCancel(t.Context()), func(tx *sql.Tx) error {
			if _, err := tx.Exec(`INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES ('k', 0, 1, 0, 0)`); err != nil {
				t.Fatal(err)
			}
			panic("test: a bug inside a transaction")
		})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, ok, err := s.RateReserve(ctx, store.RateHit{Key: "k", Limit: 1}); err != nil || !ok {
		t.Fatalf("the writer stayed locked after a panic, or the panicking transaction committed: %v %v", ok, err)
	}
}

func TestACommitAfterTheClientLeftReportsTheCancellation(t *testing.T) {
	s := openTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO rate_counters (key, window_start, count, bytes, purge_at) VALUES ('k', 0, 1, 0, 0)`); err != nil {
			return err
		}
		cancel()
		for {
			if _, err := tx.ExecContext(context.Background(), `SELECT 1`); errors.Is(err, sql.ErrTxDone) {
				return nil
			}
			time.Sleep(time.Millisecond)
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a commit after the client left: %v", err)
	}
}

func TestClaimRefusesImpossibleOpenCounters(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	mac := bytes.Repeat([]byte{7}, 32)
	create := func(id string) {
		if err := s.Create(ctx, &store.Message{ID: id, Version: 1, State: store.StateAvailable, CreatedAt: now, ExpiresAt: now.Add(time.Hour), TTLSeconds: 3600, OpensMax: 1, OpensRemaining: 1, PepperID: 7, ReadMAC: mac, ManageMAC: mac, Size: 64, BlobRef: id}); err != nil {
			t.Fatal(err)
		}
	}
	claim := func(id string) error {
		_, err := s.Claim(ctx, store.ClaimParams{ID: id, Clock: func() time.Time { return now }, LeaseMAC: mac, LeasePepperID: 7})
		return err
	}
	create("AAAAAAAAAAAAAAAAAAAAAA")
	if _, err := s.w.Exec(`UPDATE messages SET opens_remaining = 0 WHERE id = 'AAAAAAAAAAAAAAAAAAAAAA'`); err != nil {
		t.Fatal(err)
	}
	var gone *store.GoneError
	if err := claim("AAAAAAAAAAAAAAAAAAAAAA"); !errors.As(err, &gone) || gone.State != store.StateOpened {
		t.Fatalf("an available row without opens left gave an open: %v", err)
	}
	var remaining int
	if err := s.w.QueryRow(`SELECT opens_remaining FROM messages WHERE id = 'AAAAAAAAAAAAAAAAAAAAAA'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("the counter wrapped: %d %v", remaining, err)
	}
	create("AAAAAAAAAAAAAAAAAAAAAQ")
	if _, err := s.w.Exec(`UPDATE messages SET opens_max = 70000, opens_remaining = 70000 WHERE id = 'AAAAAAAAAAAAAAAAAAAAAQ'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "AAAAAAAAAAAAAAAAAAAAAQ"); !errors.Is(err, errCorruptRow) {
		t.Fatalf("a row with counters out of range was read: %v", err)
	}
	if _, err := s.Claim(ctx, store.ClaimParams{ID: "AAAAAAAAAAAAAAAAAAAAAA"}); !errors.Is(err, errClaimParams) {
		t.Fatalf("a claim without a clock: %v", err)
	}
}
