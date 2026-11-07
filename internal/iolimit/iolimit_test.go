package iolimit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/iolimit"
)

func TestAcquireWaitsOnceEverySlotIsTaken(t *testing.T) {
	for range iolimit.Slots {
		if err := iolimit.Acquire(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	waiting, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := iolimit.Acquire(waiting); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a slot beyond the limit must wait until its context ends: %v", err)
	}
	iolimit.Release()
	if err := iolimit.Acquire(t.Context()); err != nil {
		t.Fatalf("a released slot must be free again: %v", err)
	}
	for range iolimit.Slots {
		iolimit.Release()
	}
}
