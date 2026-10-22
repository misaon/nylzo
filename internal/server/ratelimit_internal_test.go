package server

import (
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/pepper"
)

func TestSourceLimiterEvictsIdleSourcesFirst(t *testing.T) {
	l := newKeyedLimiter(1, 1)
	l.max = 16
	now := time.Unix(1_800_000_000, 0)
	for i := range 3 {
		l.allow([pepper.BucketLen]byte{1, byte(i)}, now)
	}
	later := now.Add(2 * time.Second)
	for i := range 13 {
		key := [pepper.BucketLen]byte{2, byte(i)}
		if !l.allow(key, later) || l.allow(key, later) {
			t.Fatal("throttled source burst")
		}
	}
	if !l.allow([pepper.BucketLen]byte{3}, later) {
		t.Fatal("a new source was refused at the cap")
	}
	for i := range 13 {
		if l.allow([pepper.BucketLen]byte{2, byte(i)}, later) {
			t.Fatalf("eviction reset throttled source %d while idle sources were available", i)
		}
	}
}

func TestSourceLimiterKeepsMostSourcesWhenFull(t *testing.T) {
	l := newKeyedLimiter(1, 1)
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
