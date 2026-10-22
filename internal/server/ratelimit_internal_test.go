package server

import (
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/pepper"
)

func TestSourceLimiterEvictsIdleSourcesFirst(t *testing.T) {
	l := newSourceLimiter(1, 1)
	l.max = 2
	now := time.Unix(1_800_000_000, 0)
	busy, idle, fresh := [pepper.BucketLen]byte{1}, [pepper.BucketLen]byte{2}, [pepper.BucketLen]byte{3}
	l.allow(idle, now)
	later := now.Add(2 * time.Second)
	if !l.allow(busy, later) || l.allow(busy, later) {
		t.Fatal("busy source burst")
	}
	if !l.allow(fresh, later) {
		t.Fatal("a new source was refused at the cap")
	}
	if l.allow(busy, later) {
		t.Fatal("eviction reset a throttled source")
	}
}

func TestSourceLimiterKeepsMostSourcesWhenFull(t *testing.T) {
	l := newSourceLimiter(1, 1)
	l.max = 64
	now := time.Unix(1_800_000_000, 0)
	for i := range 64 {
		l.allow([pepper.BucketLen]byte{byte(i)}, now)
	}
	l.allow([pepper.BucketLen]byte{0, 1}, now)
	throttled := 0
	for i := range 64 {
		if _, ok := l.buckets[[pepper.BucketLen]byte{byte(i)}]; ok {
			throttled++
		}
	}
	if throttled < 48 || throttled == 64 {
		t.Fatalf("a full table must drop a small share of sources, kept %d of 64", throttled)
	}
}
