package server

import (
	"context"
	"net/http"
	"net/netip"
	"time"

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

// allowAPI asks the token buckets in order, so a refused source or range takes no later token.
func (s *Server) allowAPI(r *http.Request) bool {
	now := time.Now()
	address := s.clientAddress(r)
	addressRange := s.addressKey.Range(address)
	return s.sourceLimiter.Allow(s.addressKey.Source(address), now) &&
		s.rangeLimiter.Allow(addressRange, now) &&
		s.instance.Allow(addressRange, now)
}

func (s *Server) allowRead(w http.ResponseWriter, r *http.Request) bool {
	return s.allowHourly(w, r, "read", s.settings.Rates.ReadPerHour)
}

func (s *Server) allowHourly(w http.ResponseWriter, r *http.Request, kind string, limit int64) bool {
	now := time.Now()
	address := s.clientAddress(r)
	hour := ratelimit.Hour(now)
	retry := s.counters.Count([]ratelimit.Hit{
		{Kind: kind + ":h", Bucket: s.addressKey.Source(address), Window: hour, Limit: limit},
		{
			Kind:   kind + ":h:c",
			Bucket: s.addressKey.Range(address),
			Window: hour,
			Limit:  limit * s.settings.Rates.CoarseMultiplier,
		},
	}, now)
	if retry > 0 {
		writeRateLimited(w, retry)
		return false
	}
	return true
}

// takeStream holds one of the slots that bound claim and blob responses; it answers 503 when none is free.
func (s *Server) takeStream(w http.ResponseWriter, r *http.Request) (func(), bool) {
	address := s.clientAddress(r)
	source, addressRange := s.addressKey.Source(address), s.addressKey.Range(address)
	if !s.streams.Take(source, addressRange) {
		setRetryAfter(w, streamBusyRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return nil, false
	}
	return func() { s.streams.Release(source, addressRange) }, true
}

func (s *Server) limitCreate(now time.Time, address netip.Addr, size int64) time.Duration {
	source, addressRange := s.addressKey.Source(address), s.addressKey.Range(address)
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
		hits = append(hits,
			ratelimit.Hit{Kind: "create:d:share", Bucket: addressRange, Window: day, Limit: max(minimumCapShare, daily/rangeCapShare)},
			ratelimit.Hit{
				Kind:   "create:d:wide",
				Bucket: s.addressKey.Wide(address),
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
}

func (s *Server) globalGate(ctx context.Context, now time.Time) (globalGate, error) {
	limit := s.settings.Rates.GlobalDailyCreates
	if limit <= 0 {
		return globalGate{}, nil
	}
	creates, err := s.store.RateGet(ctx, globalCreatesKey, ratelimit.Day(now).Start)
	return globalGate{full: creates.Count >= limit, workNeeded: s.workNeededAfter(creates.Count)}, err
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
		s.logger.ErrorContext(ctx, "global-release", "err", logging.ErrorText(err))
	}
}

func (s *Server) storedCreateCounters(now time.Time) []store.RateHit {
	if s.settings.Rates.GlobalDailyCreates <= 0 {
		return nil
	}
	day := ratelimit.Day(now)
	return []store.RateHit{{Key: globalStoredKey, WindowStart: day.Start, PurgeAt: day.PurgeAt()}}
}
