package sqlite_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

func BenchmarkGet(b *testing.B) {
	storage := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
	message := newMessage(1, false)
	create(b, storage, message)
	for b.Loop() {
		if _, err := storage.Get(b.Context(), message.ID); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLeases(b *testing.B) {
	storage := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
	message := newMessage(3, false)
	create(b, storage, message)
	for range 3 {
		claim(b, storage, message, testNow)
	}
	for b.Loop() {
		if leases, err := storage.Leases(b.Context(), message.ID); err != nil || len(leases) != 3 {
			b.Fatal(len(leases), err)
		}
	}
}

func BenchmarkRateHit(b *testing.B) {
	storage := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
	hits := []store.RateHit{
		{Key: "bench:h", WindowStart: testNow, PurgeAt: testNow.Add(2 * time.Hour)},
		{Key: "bench:d", WindowStart: testNow, PurgeAt: testNow.Add(48 * time.Hour)},
	}
	for b.Loop() {
		if _, err := storage.RateHit(b.Context(), hits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRateReserve(b *testing.B) {
	storage := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
	hit := store.RateHit{
		Key:         "global:creates:d",
		WindowStart: testNow,
		PurgeAt:     testNow.Add(48 * time.Hour),
		Limit:       1 << 62,
	}
	for b.Loop() {
		if _, ok, err := storage.RateReserve(b.Context(), hit); err != nil || !ok {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkRateReserveBatch(b *testing.B) {
	storage := openStore(b, filepath.Join(b.TempDir(), "nylzo.db"))
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
		if _, err := storage.RateReserveBatch(b.Context(), hits); err != nil {
			b.Fatal(err)
		}
	}
}
