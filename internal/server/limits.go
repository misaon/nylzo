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

const prefixShare = 0.5

const (
	globalCreatesKey = "global:d"
	globalStoredKey  = "global:stored:d"
)

func (s *Server) limitCreate(now time.Time, addr netip.Addr, size int64) time.Duration {
	fine, coarse := s.addressKey.Source(addr), s.addressKey.Range(addr)
	h, d := ratelimit.Hour(now), ratelimit.Day(now)
	r := s.cfg.Rates
	m := r.CoarseMultiplier
	return s.counters.Count([]ratelimit.Hit{
		{Kind: "create:h", Bucket: fine, Window: h, Limit: r.CreatePerHour},
		{
			Kind:       "create:d",
			Bucket:     fine,
			Window:     d,
			Bytes:      size,
			Limit:      r.CreatePerDay,
			BytesLimit: r.CreateBytesPerDay,
		},
		{Kind: "create:h:c", Bucket: coarse, Window: h, Limit: r.CreatePerHour * m},
		{
			Kind:       "create:d:c",
			Bucket:     coarse,
			Window:     d,
			Bytes:      size,
			Limit:      r.CreatePerDay * m,
			BytesLimit: r.CreateBytesPerDay * m,
		},
	}, now)
}

type globalState struct {
	full      bool
	powNeeded bool
}

func (s *Server) globalGate(ctx context.Context, now time.Time) (globalState, error) {
	limit := s.cfg.Rates.GlobalDailyCreates
	if limit <= 0 {
		return globalState{}, nil
	}
	c, err := s.store.RateGet(ctx, globalCreatesKey, ratelimit.Day(now).Start)
	return globalState{full: c.Count >= limit, powNeeded: s.powNeededAfter(c.Count)}, err
}

func (s *Server) powNeededAfter(creates int64) bool {
	limit := s.cfg.Rates.GlobalDailyCreates
	return limit > 0 && creates*2 >= limit
}

func (s *Server) reserveGlobalCreate(ctx context.Context, now time.Time) (slot int64, ok bool, err error) {
	limit := s.cfg.Rates.GlobalDailyCreates
	if limit <= 0 {
		return 0, true, nil
	}
	d := ratelimit.Day(now)
	return s.store.RateReserve(
		context.WithoutCancel(ctx),
		store.RateHit{Key: globalCreatesKey, WindowStart: d.Start, PurgeAt: d.PurgeAt(), Limit: limit},
	)
}

func (s *Server) releaseGlobalCreate(ctx context.Context, now time.Time) {
	if s.cfg.Rates.GlobalDailyCreates <= 0 {
		return
	}
	if err := s.store.RateRelease(context.WithoutCancel(ctx), globalCreatesKey, ratelimit.Day(now).Start); err != nil {
		s.log.Error("global-release", "err", logging.ErrorText(err))
	}
}

func (s *Server) storedCreateCounters(now time.Time) []store.RateHit {
	if s.cfg.Rates.GlobalDailyCreates <= 0 {
		return nil
	}
	d := ratelimit.Day(now)
	return []store.RateHit{{Key: globalStoredKey, WindowStart: d.Start, PurgeAt: d.PurgeAt()}}
}

func (s *Server) limitHourly(now time.Time, addr netip.Addr, kind string, limit int64) time.Duration {
	h := ratelimit.Hour(now)
	return s.counters.Count([]ratelimit.Hit{
		{Kind: kind + ":h", Bucket: s.addressKey.Source(addr), Window: h, Limit: limit},
		{
			Kind:   kind + ":h:c",
			Bucket: s.addressKey.Range(addr),
			Window: h,
			Limit:  limit * s.cfg.Rates.CoarseMultiplier,
		},
	}, now)
}

func (s *Server) clientAddr(r *http.Request) netip.Addr {
	return s.proxies.ClientAddress(r)
}
