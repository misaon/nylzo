package sqlite_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

func BenchmarkGet(b *testing.B) {
	s := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
	message := newMessage(1, false)
	create(b, s, message)
	for b.Loop() {
		if _, err := s.Get(b.Context(), message.ID); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLeases(b *testing.B) {
	s := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
	message := newMessage(3, false)
	create(b, s, message)
	for range 3 {
		claim(b, s, message, testNow)
	}
	for b.Loop() {
		if leases, err := s.Leases(b.Context(), message.ID); err != nil || len(leases) != 3 {
			b.Fatal(len(leases), err)
		}
	}
}

func BenchmarkRateHit(b *testing.B) {
	s := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
	hits := []store.RateHit{
		{Key: "bench:h", WindowStart: testNow, PurgeAt: testNow.Add(2 * time.Hour)},
		{Key: "bench:d", WindowStart: testNow, PurgeAt: testNow.Add(48 * time.Hour)},
	}
	for b.Loop() {
		if _, err := s.RateHit(b.Context(), hits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRateReserve(b *testing.B) {
	s := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
	hit := store.RateHit{
		Key:         "global:creates:d",
		WindowStart: testNow,
		PurgeAt:     testNow.Add(48 * time.Hour),
		Limit:       1 << 62,
	}
	for b.Loop() {
		if _, ok, err := s.RateReserve(b.Context(), hit); err != nil || !ok {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkRateReserveBatch(b *testing.B) {
	s := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
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
