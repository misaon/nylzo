package ratelimit

import (
	"net/netip"
	"testing"
	"time"
)

// A memory image shows no address bucket of an earlier window, and a new day's key gives the same address another
// bucket.
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

// 40 ranges at 10 requests a second and a bystander at one a second share an instance bucket of 50 a second.
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

// 200 ranges at one request a second and a bystander that asks once every 25 seconds share an instance bucket of 10
// a second for 1000 seconds.
func TestARangeWithinItsShareGetsThroughWhenRangesOutnumberTheRate(t *testing.T) {
	t.Parallel()
	fair := NewFair(10, 20)
	start := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	bystander, served, asked, admitted := Bucket{0xff, 0xff}, 0, 0, 0
	for step := range 100_000 {
		now := start.Add(time.Duration(step) * 10 * time.Millisecond)
		for attacker := range 200 {
			if (step+attacker*7)%100 == 0 && fair.Allow(Bucket{byte(attacker), 1}, now) {
				admitted++
			}
		}
		if step%2500 == 1251 {
			asked++
			if fair.Allow(bystander, now) {
				served++
			}
		}
	}
	if served*100 < asked*95 || admitted+served > 10*1000+20 {
		t.Fatalf("the bystander got %d of %d requests through, all ranges %d", served, asked, admitted+served)
	}
}

func TestAFullFairTableStillAdmitsANewRange(t *testing.T) {
	t.Parallel()
	fair := NewFair(10, 20)
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for i := range maxTableEntries {
		fair.Allow(Bucket{byte(i), byte(i >> 8), 1}, now)
	}
	if !fair.Allow(Bucket{0xff, 0xff, 0xff}, now.Add(500*time.Millisecond)) {
		t.Fatal("a full table refused a new range whose turn was due")
	}
}

func TestADayResetEmptiesEveryAddressTable(t *testing.T) {
	t.Parallel()
	now := time.Now()
	bucket := Bucket{1, 2, 3}
	limiter, counters, fair, streams := NewLimiter(10, 20), NewCounters(), NewFair(50, 100), NewStreams(8)
	limiter.Allow(bucket, now)
	counters.Count([]Hit{{Kind: "read:h", Bucket: bucket, Window: Hour(now), Limit: 300}}, now)
	fair.Allow(bucket, now)
	streams.Take(bucket, bucket)
	limiter.Reset()
	counters.Reset()
	fair.Reset()
	if len(limiter.buckets) != 0 || len(counters.tables) != 0 || len(fair.ranges) != 0 {
		t.Fatal("a reset kept an address bucket")
	}
	streams.Release(bucket, bucket)
	streams.Purge()
	if len(streams.perSource) != 0 || len(streams.perRange) != 0 {
		t.Fatal("a released stream slot stayed in memory")
	}
}

func TestPurgesKeepOnlyWhatStillLimits(t *testing.T) {
	t.Parallel()
	now := time.Now()
	quiet, busy := Bucket{1}, Bucket{2}
	fair := NewFair(50, 100)
	fair.Allow(quiet, now)
	fair.Purge(now)
	if _, kept := fair.ranges[quiet]; !kept {
		t.Fatal("the purge dropped a range seen just now")
	}
	fair.Purge(now.Add(activeRangeWindow))
	if len(fair.ranges) != 0 {
		t.Fatal("the purge kept a range that no longer counts")
	}
	streams := NewStreams(8)
	streams.Take(quiet, quiet)
	streams.Take(busy, quiet)
	streams.Release(quiet, quiet)
	streams.Purge()
	if streams.perSource[busy] != 1 || streams.perRange[quiet] != 1 || len(streams.perSource) != 1 {
		t.Fatalf("after the purge: sources %v, ranges %v", streams.perSource, streams.perRange)
	}
}
