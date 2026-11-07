package server

import (
	"context"
	"net/http"
	"net/netip"
	"time"

	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	rangeShare       = 0.5
	streamBusyRetry  = 5 * time.Second
	rangeCapShare    = 20
	minimumCapShare  = 10
	wideCapShare     = 5
	globalCreatesKey = "global:d"
	globalStoredKey  = "global:stored:d"
)

func (s *Server) clientAddress(r *http.Request) netip.Addr {
	return s.proxies.ClientAddress(r)
}

// clientBuckets holds the /32 or /64 source and the /24 or /48 range of a request's client; each is an HMAC, so a
// request computes them once.
type clientBuckets struct {
	address      netip.Addr
	source       ratelimit.Bucket
	addressRange ratelimit.Bucket
}

type clientBucketsKey struct{}

func (s *Server) newClientBuckets(r *http.Request) *clientBuckets {
	address := s.clientAddress(r)
	return &clientBuckets{
		address:      address,
		source:       s.addressKey.Source(address),
		addressRange: s.addressKey.Range(address),
	}
}

func withClientBuckets(r *http.Request, buckets *clientBuckets) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), clientBucketsKey{}, buckets))
}

func (s *Server) bucketsOf(r *http.Request) *clientBuckets {
	if buckets, ok := r.Context().Value(clientBucketsKey{}).(*clientBuckets); ok {
		return buckets
	}
	return s.newClientBuckets(r)
}

// allowAPI asks the token buckets in order, so a refused source or range takes no later token.
func (s *Server) allowAPI(buckets *clientBuckets) bool {
	now := time.Now()
	return s.sourceLimiter.Allow(buckets.source, now) &&
		s.rangeLimiter.Allow(buckets.addressRange, now) &&
		s.instance.Allow(buckets.addressRange, now)
}

func (s *Server) allowRead(w http.ResponseWriter, r *http.Request) bool {
	return s.allowHourly(w, r, "read", s.settings.Rates.ReadPerHour)
}

func (s *Server) allowHourly(w http.ResponseWriter, r *http.Request, kind string, limit int64) bool {
	return s.countHourly(w, r, ratelimit.Hit{Kind: kind, Limit: limit})
}

// allowReadBytes counts the object that a claim or download is about to stream, so a few clients cannot fill the
// uplink that every other reader shares.
func (s *Server) allowReadBytes(w http.ResponseWriter, r *http.Request, size int64) bool {
	limit := s.settings.Rates.ReadBytesPerHour
	if limit <= 0 {
		return true
	}
	return s.countHourly(w, r, ratelimit.Hit{Kind: "read-bytes", Bytes: size, BytesLimit: limit})
}

// countHourly counts a hit against the hour of its /32 or /64 source and, with limits times the coarse multiplier,
// of its /24 or /48 range.
func (s *Server) countHourly(w http.ResponseWriter, r *http.Request, hit ratelimit.Hit) bool {
	now := time.Now()
	buckets := s.bucketsOf(r)
	coarse := s.settings.Rates.CoarseMultiplier
	source := hit
	source.Kind, source.Bucket, source.Window = hit.Kind+":h", buckets.source, ratelimit.Hour(now)
	addressRange := source
	addressRange.Kind, addressRange.Bucket = hit.Kind+":h:c", buckets.addressRange
	addressRange.Limit, addressRange.BytesLimit = hit.Limit*coarse, hit.BytesLimit*coarse
	if retry := s.counters.Count([]ratelimit.Hit{source, addressRange}, now); retry > 0 {
		writeRateLimited(w, retry)
		return false
	}
	return true
}

// takeStream holds one of the slots that bound claim and blob responses; it answers 503 when none is free.
func (s *Server) takeStream(w http.ResponseWriter, r *http.Request) (func(), bool) {
	buckets := s.bucketsOf(r)
	source, addressRange := buckets.source, buckets.addressRange
	if !s.streams.Take(source, addressRange) {
		setRetryAfter(w, streamBusyRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return nil, false
	}
	return func() { s.streams.Release(source, addressRange) }, true
}

func (s *Server) limitCreate(now time.Time, buckets *clientBuckets, size int64) time.Duration {
	source, addressRange := buckets.source, buckets.addressRange
	hour, day := ratelimit.Hour(now), ratelimit.Day(now)
	rates := s.settings.Rates
	coarse := rates.CoarseMultiplier
	hits := []ratelimit.Hit{
		{Kind: "create:h", Bucket: source, Window: hour, Limit: rates.CreatePerHour},
		{
			Kind:       "create:d",
			Bucket:     source,
			Window:     day,
			Bytes:      size,
			Limit:      rates.CreatePerDay,
			BytesLimit: rates.CreateBytesPerDay,
		},
		{Kind: "create:h:c", Bucket: addressRange, Window: hour, Limit: rates.CreatePerHour * coarse},
		{
			Kind:       "create:d:c",
			Bucket:     addressRange,
			Window:     day,
			Bytes:      size,
			Limit:      rates.CreatePerDay * coarse,
			BytesLimit: rates.CreateBytesPerDay * coarse,
		},
	}
	if daily := rates.GlobalDailyCreates; daily > 0 {
		hits = append(
			hits,
			ratelimit.Hit{
				Kind:   "create:d:share",
				Bucket: addressRange,
				Window: day,
				Limit:  max(minimumCapShare, daily/rangeCapShare),
			},
			ratelimit.Hit{
				Kind:   "create:d:wide",
				Bucket: s.addressKey.Wide(buckets.address),
				Window: day,
				Limit:  max(minimumCapShare, daily/wideCapShare),
			},
		)
	}
	return s.counters.Count(hits, now)
}

type globalGate struct {
	full       bool
	workNeeded bool
	difficulty int
}

func (s *Server) globalGate(ctx context.Context, now time.Time) (globalGate, error) {
	limit := s.settings.Rates.GlobalDailyCreates
	if limit <= 0 {
		return globalGate{}, nil
	}
	creates, err := s.store.RateGet(ctx, globalCreatesKey, ratelimit.Day(now).Start)
	return globalGate{
		full:       creates.Count >= limit,
		workNeeded: s.workNeededAfter(creates.Count),
		difficulty: s.workDifficulty(creates.Count),
	}, err
}

// workDifficulty adds one to the configured difficulty for each tenth of the cap taken past half, up to the most a
// client accepts, so each create costs more the fuller the cap gets.
func (s *Server) workDifficulty(creates int64) int {
	base, limit := s.settings.PoWDifficulty, s.settings.Rates.GlobalDailyCreates
	if limit <= 0 {
		return base
	}
	tenthsPastHalf := max(0, (creates*2-limit)*5/limit)
	return int(min(int64(proofofwork.MaxDifficulty), int64(base)+tenthsPastHalf))
}

func (s *Server) workNeededAfter(creates int64) bool {
	limit := s.settings.Rates.GlobalDailyCreates
	return limit > 0 && creates*2 >= limit
}

type globalSlot struct {
	number   int64
	reserved bool
}

func (s *Server) reserveGlobalCreate(ctx context.Context, now time.Time) (globalSlot, error) {
	limit := s.settings.Rates.GlobalDailyCreates
	if limit <= 0 {
		return globalSlot{reserved: true}, nil
	}
	day := ratelimit.Day(now)
	number, err := s.store.RateReserve(
		context.WithoutCancel(ctx),
		store.RateHit{Key: globalCreatesKey, WindowStart: day.Start, PurgeAt: day.PurgeAt(), Limit: limit},
	)
	return globalSlot{number: number, reserved: number > 0}, err
}

func (s *Server) releaseGlobalCreate(ctx context.Context, now time.Time) {
	if s.settings.Rates.GlobalDailyCreates <= 0 {
		return
	}
	err := s.store.RateRelease(context.WithoutCancel(ctx), globalCreatesKey, ratelimit.Day(now).Start)
	if err != nil {
		s.events.ErrorContext(ctx, "global-release", "err", logging.ErrorText(err))
	}
}

// ReturnUnstoredCreates gives back the cap slots that a crashed or killed process left taken, by setting the counter
// of the day to the messages stored. It runs before the server listens, while no upload of this data directory is in
// flight, since one process serves a data directory at a time.
func (s *Server) ReturnUnstoredCreates(ctx context.Context) error {
	if s.settings.Rates.GlobalDailyCreates <= 0 {
		return nil
	}
	return s.store.RateCopy(ctx, globalStoredKey, globalCreatesKey, ratelimit.Day(time.Now()).Start)
}

func (s *Server) storedCreateCounters(now time.Time) []store.RateHit {
	if s.settings.Rates.GlobalDailyCreates <= 0 {
		return nil
	}
	day := ratelimit.Day(now)
	return []store.RateHit{{Key: globalStoredKey, WindowStart: day.Start, PurgeAt: day.PurgeAt()}}
}
