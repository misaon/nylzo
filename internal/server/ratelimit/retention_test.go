package ratelimit

import (
	"net/netip"
	"testing"
	"time"
)

// TestTablesKeepOnlyBucketsThatStillLimit covers A4-3: a memory image shows no address bucket of an earlier window,
// and a new day's key gives the same address another bucket.
func TestTablesKeepOnlyBucketsThatStillLimit(t *testing.T) {
	t.Parallel()
	key := NewAddressKey()
	address := netip.MustParseAddr("198.51.100.23")
	limiter, counters := NewLimiter(10, 20), NewCounters()
	now := time.Now()
	limiter.Allow(key.Source(address), now)
	counters.Count([]Hit{{Kind: "read:h", Bucket: key.Source(address), Window: Hour(now), Limit: 300}}, now)
	limiter.Purge(now)
	counters.Purge(now)
	if len(limiter.buckets) != 1 || len(counters.tables["read:h"].entries) != 1 {
		t.Fatal("a bucket that still limits was dropped")
	}
	later := Hour(now).End
	limiter.Purge(later)
	counters.Purge(later)
	if len(limiter.buckets) != 0 || len(counters.tables["read:h"].entries) != 0 {
		t.Fatal("a refilled token bucket or an ended window is still in memory")
	}
	before := key.Source(address)
	key.Rotate()
	if key.Source(address) == before {
		t.Fatal("the next day's key gave the address its old bucket")
	}
}

// TestManyRangesCannotTakeTheInstanceFromAQuietOne covers F4: 40 ranges at 10 requests a second and a bystander at
// one a second share an instance bucket of 50 a second.
func TestManyRangesCannotTakeTheInstanceFromAQuietOne(t *testing.T) {
	t.Parallel()
	fair := NewFair(50, 100)
	start := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	bystander, served, asked, admitted := Bucket{0xff}, 0, 0, 0
	for step := range 25_000 {
		now := start.Add(time.Duration(step) * time.Millisecond)
		for attacker := range 40 {
			if (step+attacker*2)%100 == 0 && fair.Allow(Bucket{byte(attacker)}, now) {
				admitted++
			}
		}
		if step%1000 == 500 {
			asked++
			if fair.Allow(bystander, now) {
				served++
			}
		}
	}
	if served*100 < asked*95 || admitted+served > 50*25+100 {
		t.Fatalf("the bystander got %d of %d requests through, all ranges %d", served, asked, admitted+served)
	}
}
