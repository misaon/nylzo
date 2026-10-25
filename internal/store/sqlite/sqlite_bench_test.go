package sqlite_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func benchStore(b *testing.B) *sqlite.Store {
	b.Helper()
	s, err := sqlite.Open(b.Context(), filepath.Join(b.TempDir(), "nylzo.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	return s
}

func BenchmarkGet(b *testing.B) {
	s := benchStore(b)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	id := core.NewID().String()
	mac := bytes.Repeat([]byte{7}, 32)
	if err := s.Create(
		b.Context(),
		&store.Message{
			ID:             id,
			Version:        1,
			State:          store.StateAvailable,
			CreatedAt:      now,
			ExpiresAt:      now.Add(time.Hour),
			TTLSeconds:     3600,
			OpensMax:       1,
			OpensRemaining: 1,
			PepperID:       7,
			ReadMAC:        mac,
			ManageMAC:      mac,
			Size:           64,
			BlobRef:        id,
		},
	); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := s.Get(b.Context(), id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRateReserveBatch(b *testing.B) {
	s := benchStore(b)
	hits := make([]store.RateHit, 256)
	round := 0
	for b.Loop() {
		for i := range hits {
			hits[i] = store.RateHit{
				Key:         fmt.Sprintf("challenge:%d:%d", round, i),
				WindowStart: time.Unix(0, 0),
				PurgeAt:     time.Unix(4102444800, 0),
				Limit:       1,
			}
		}
		round++
		if _, err := s.RateReserveBatch(b.Context(), hits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLeases(b *testing.B) {
	s := benchStore(b)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	id := core.NewID().String()
	mac := bytes.Repeat([]byte{7}, 32)
	if err := s.Create(
		b.Context(),
		&store.Message{
			ID:             id,
			Version:        1,
			State:          store.StateAvailable,
			CreatedAt:      now,
			ExpiresAt:      now.Add(time.Hour),
			TTLSeconds:     3600,
			OpensMax:       3,
			OpensRemaining: 3,
			PepperID:       7,
			ReadMAC:        mac,
			ManageMAC:      mac,
			Size:           64,
			BlobRef:        id,
		},
	); err != nil {
		b.Fatal(err)
	}
	for range 3 {
		if _, err := s.Claim(
			b.Context(),
			store.ClaimParams{ID: id, Clock: func() time.Time { return now }, LeaseMAC: mac, LeasePepperID: 7},
		); err != nil {
			b.Fatal(err)
		}
	}
	for b.Loop() {
		if leases, err := s.Leases(b.Context(), id); err != nil || len(leases) != 3 {
			b.Fatal(len(leases), err)
		}
	}
}

func BenchmarkRateHit(b *testing.B) {
	s := benchStore(b)
	window := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	hits := []store.RateHit{
		{Key: "bench:h", WindowStart: window, PurgeAt: window.Add(2 * time.Hour)},
		{Key: "bench:d", WindowStart: window, PurgeAt: window.Add(48 * time.Hour)},
	}
	for b.Loop() {
		if _, err := s.RateHit(b.Context(), hits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRateReserve(b *testing.B) {
	s := benchStore(b)
	window := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	hit := store.RateHit{
		Key:         "global:creates:d",
		WindowStart: window,
		PurgeAt:     window.Add(48 * time.Hour),
		Limit:       1 << 62,
	}
	for b.Loop() {
		if _, ok, err := s.RateReserve(b.Context(), hit); err != nil || !ok {
			b.Fatal(ok, err)
		}
	}
}
