package ratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestAddressBucketsGroupSourcesAndRanges(t *testing.T) {
	t.Parallel()
	first, second := NewAddressKey(), NewAddressKey()
	address := netip.MustParseAddr("203.0.113.77")
	if first.Source(address) == second.Source(address) && first.Range(address) == second.Range(address) {
		t.Fatal("two processes derived the same buckets, so the key is not random per process")
	}
	key := NewAddressKey()
	for _, pair := range []struct {
		one, other   string
		sameSource   bool
		sameRange    bool
		relationship string
	}{
		{"203.0.113.77", "203.0.113.78", false, true, "two addresses of one /24"},
		{"203.0.113.77", "::ffff:203.0.113.77", true, true, "an IPv4 address and its mapped form"},
		{"203.0.113.77", "203.0.114.77", false, false, "two /24s"},
		{"2001:db8:1:2:3:4:5:6", "2001:db8:1:2:ffff:ffff:ffff:ffff", true, true, "two addresses of one /64"},
		{"2001:db8:1:2::1", "2001:db8:1:3::1", false, true, "two /64s of one /48"},
		{"2001:db8:1::1", "2001:db8:1:ffff:ffff:ffff:ffff:ffff", false, true, "both ends of one /48"},
		{"2001:db8:1::1", "2001:db8:0:ffff::1", false, false, "two /48s"},
	} {
		one, other := netip.MustParseAddr(pair.one), netip.MustParseAddr(pair.other)
		if (key.Source(one) == key.Source(other)) != pair.sameSource ||
			(key.Range(one) == key.Range(other)) != pair.sameRange {
			t.Errorf("%s: same source %v, same range %v", pair.relationship, pair.sameSource, pair.sameRange)
		}
	}
}

func TestLimiterRefillsAtItsRate(t *testing.T) {
	t.Parallel()
	limiter := NewLimiter(1, 2)
	var key Bucket
	now := limiter.base
	for i := range 2 {
		if !limiter.Allow(key, now) {
			t.Fatalf("burst token %d refused", i)
		}
	}
	for _, step := range []struct {
		after time.Duration
		want  bool
	}{{500 * time.Millisecond, false}, {1100 * time.Millisecond, true}, {1200 * time.Millisecond, false}} {
		if limiter.Allow(key, now.Add(step.after)) != step.want {
			t.Fatalf("after %v at 1 request per second: allowed %v", step.after, !step.want)
		}
	}
}

func TestFullLimiterDropsIdleSourcesFirst(t *testing.T) {
	t.Parallel()
	limiter := NewLimiter(1, 1)
	limiter.maxEntries = 16
	now := time.Unix(1_800_000_000, 0)
	for i := range 3 {
		limiter.Allow(Bucket{1, byte(i)}, now)
	}
	later := now.Add(2 * time.Second)
	for i := range 13 {
		key := Bucket{2, byte(i)}
		if !limiter.Allow(key, later) || limiter.Allow(key, later) {
			t.Fatal("throttled source burst")
		}
	}
	if !limiter.Allow(Bucket{3}, later) {
		t.Fatal("a new source was refused at the cap")
	}
	for i := range 13 {
		if limiter.Allow(Bucket{2, byte(i)}, later) {
			t.Fatalf("trimming reset throttled source %d while idle sources were available", i)
		}
	}
	busy := NewLimiter(1, 1)
	busy.maxEntries = 64
	for i := range 64 {
		busy.Allow(Bucket{byte(i)}, now)
	}
	busy.Allow(Bucket{0, 1}, now)
	if kept := len(busy.buckets) - 1; kept < 48 || kept == 64 {
		t.Fatalf("a full table of throttled sources must drop a small share, kept %d of 64", kept)
	}
}

func TestCountersRefuseUntilTheWindowEnds(t *testing.T) {
	t.Parallel()
	counters := NewCounters()
	window := Hour(time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC))
	hits := []Hit{{Kind: "create:h", Window: window, Limit: 1}}
	if retry := counters.Count(hits, window.Start); retry != 0 {
		t.Fatalf("first request refused: %v", retry)
	}
	if retry := counters.Count(hits, window.Start.Add(time.Minute)); retry != 59*time.Minute {
		t.Fatalf("a refused request must wait until its window ends: %v", retry)
	}
	if retry := counters.Count(hits, window.End.Add(-time.Nanosecond)); retry != time.Second {
		t.Fatalf("a request refused just before the window ends got Retry-After %v", retry)
	}
	next := []Hit{{Kind: "create:h", Window: Hour(window.End), Limit: 1}}
	if retry := counters.Count(next, window.End); retry != 0 {
		t.Fatalf("the next window still counts the old requests: %v", retry)
	}
}

func TestCountersStopAtTheFirstRefusal(t *testing.T) {
	t.Parallel()
	counters := NewCounters()
	window := Hour(time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC))
	source, addressRange := Bucket{1}, Bucket{2}
	hits := []Hit{
		{Kind: "create:d", Bucket: source, Window: window, Bytes: 10, BytesLimit: 10},
		{Kind: "create:d:c", Bucket: addressRange, Window: window, Limit: 10},
	}
	for range 3 {
		counters.Count(hits, window.Start)
	}
	if got := counters.tables["create:d:c"].entries[addressRange].count; got != 1 {
		t.Fatalf("refused requests were counted against the range: %d", got)
	}
}

func TestCountersStayBounded(t *testing.T) {
	t.Parallel()
	counters := NewCounters()
	counters.maxEntries = 64
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	old, current := Hour(now.Add(-time.Hour)), Hour(now)
	for i := range counters.maxEntries {
		counters.Count([]Hit{{Kind: "k", Bucket: Bucket{byte(i)}, Window: old}}, old.Start)
	}
	counters.Count([]Hit{{Kind: "k", Bucket: Bucket{0xff, 0xff}, Window: current}}, now)
	if n := len(counters.tables["k"].entries); n != 1 {
		t.Fatalf("expired buckets were kept: %d", n)
	}
	for i := range 4 * counters.maxEntries {
		counters.Count([]Hit{{Kind: "k", Bucket: Bucket{byte(i), byte(i >> 8), 1}, Window: current}}, now)
	}
	if n := len(counters.tables["k"].entries); n > counters.maxEntries {
		t.Fatalf("the table grew past its bound: %d", n)
	}
}

func TestStreamsLimitEachSourceAndRange(t *testing.T) {
	t.Parallel()
	streams := NewStreams(64)
	addressRange := Bucket{9}
	for source := range byte(2) {
		for slot := range 2 {
			if !streams.Take(Bucket{source}, addressRange) {
				t.Fatalf("source %d could not take slot %d", source, slot)
			}
		}
	}
	if streams.Take(Bucket{0}, addressRange) {
		t.Fatal("a source took a third slot")
	}
	for source := byte(2); source < 4; source++ {
		streams.Take(Bucket{source}, addressRange)
		streams.Take(Bucket{source}, addressRange)
	}
	if streams.Take(Bucket{5}, addressRange) || !streams.Take(Bucket{5}, Bucket{10}) {
		t.Fatal("a range must hold at most 8 slots while other ranges still get theirs")
	}
	streams.Release(Bucket{0}, addressRange)
	if !streams.Take(Bucket{6}, addressRange) {
		t.Fatal("a released slot was not free again")
	}
	whole := NewStreams(1)
	if !whole.Take(Bucket{1}, Bucket{1}) || whole.Take(Bucket{2}, Bucket{2}) {
		t.Fatal("the instance limit did not hold")
	}
	whole.Release(Bucket{1}, Bucket{1})
	if whole.total != 0 || len(whole.perSource) != 0 || len(whole.perRange) != 0 {
		t.Fatalf("released slots are still held: %+v", whole)
	}
}

func TestClientAddress(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("fd00::/8"),
		netip.MustParsePrefix("172.17.0.1/32"),
	}
	for _, c := range []struct {
		remote, forwardedFor, realIP string
		readRealIP                   bool
		want                         string
	}{
		{"203.0.113.1:1234", "", "", false, "203.0.113.1"},
		{"203.0.113.1:1234", "198.51.100.1", "", false, "203.0.113.1"},
		{"10.0.0.5:1234", "198.51.100.1", "", false, "198.51.100.1"},
		{"172.17.0.1:1234", "198.51.100.9", "", false, "198.51.100.9"},
		{"10.0.0.5:1234", "198.51.100.1, 10.0.0.9", "", false, "198.51.100.1"},
		{"10.0.0.5:1234", "192.0.2.4, 198.51.100.1, 10.0.0.9", "", false, "198.51.100.1"},
		{"10.0.0.5:1234", "garbage", "", false, "10.0.0.5"},
		{"10.0.0.5:1234", "", "", false, "10.0.0.5"},
		{"[::ffff:203.0.113.7]:1234", "", "", false, "203.0.113.7"},
		{"203.0.113.1:1234", "", "198.51.100.2", false, "203.0.113.1"},
		{"10.0.0.5:1234", "", "198.51.100.2", false, "10.0.0.5"},
		{"[fd00::5]:1234", "2001:db8::7, fd00::9", "", false, "2001:db8::7"},
		{"[2001:db8::1]:1234", "2001:db8::7", "", false, "2001:db8::1"},
		{"10.0.0.5:1234", "198.51.100.1", "198.51.100.2", true, "198.51.100.2"},
		{"203.0.113.1:1234", "", "198.51.100.2", true, "203.0.113.1"},
		{"10.0.0.5:1234", "", "198.51.100.2\n198.51.100.3", true, "10.0.0.5"},
		{"10.0.0.5:1234", "", "198.51.100.2, 198.51.100.3", true, "10.0.0.5"},
		{"203.0.113.1:1234", "", "", true, "203.0.113.1"},
	} {
		name := fmt.Sprintf("real=%v/%s/xff=%q/real=%q", c.readRealIP, c.remote, c.forwardedFor, c.realIP)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			r.RemoteAddr = c.remote
			if c.forwardedFor != "" {
				r.Header.Set("X-Forwarded-For", c.forwardedFor)
			}
			for value := range strings.SplitSeq(c.realIP, "\n") {
				if value != "" {
					r.Header.Add("X-Real-IP", value)
				}
			}
			proxies := Proxies{Trusted: trusted, ReadRealIP: c.readRealIP}
			if got := proxies.ClientAddress(r); got.String() != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
	open := Proxies{ReadRealIP: true}
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "203.0.113.1:1234"
	r.Header.Set("X-Real-IP", "198.51.100.2")
	if got := open.ClientAddress(r); got.String() != "198.51.100.2" {
		t.Fatalf("X-Real-IP without trusted proxies must count: %s", got)
	}
}
