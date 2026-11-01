package server

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/diskspace"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/proof"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/sweep"
	"github.com/misaon/nylzo/internal/server/webclient"
	"github.com/misaon/nylzo/internal/store"
)

const (
	readHeaderTimeout   = 5 * time.Second
	readTimeout         = 10 * time.Second
	createBodyTimeout   = 120 * time.Second
	writeTimeout        = 120 * time.Second
	idleTimeout         = 30 * time.Second
	maxHeaderBytes      = 4 << 10
	h2cStreamBuffer     = 64 << 10
	h2cConnectionBuffer = 1 << 20
	h2cMaxFrame         = 16 << 10
)

type Option func(*Server)

func WithLogger(logger *slog.Logger) Option {
	return func(server *Server) {
		if logger != nil {
			server.logger = logger
		}
	}
}

type Server struct {
	settings      config.Server
	store         store.Store
	blobs         store.Blobs
	ring          *pepper.Ring
	leaseTimers   *releases
	logger        *slog.Logger
	instance      *ratelimit.Fair
	sourceLimiter *ratelimit.Limiter
	rangeLimiter  *ratelimit.Limiter
	addressKey    *ratelimit.AddressKey
	proxies       ratelimit.Proxies
	counters      *ratelimit.Counters
	streams       *ratelimit.Streams
	forwardedHour atomic.Int64
	challenges    *proof.Challenges
	work          *proof.Work
	idTokens      proof.IDTokens
	disk          *diskspace.Reserver
	reports       *abuse.Reports
	dummyRow      store.Row
	webFiles      fs.FS
	webBuild      webclient.Build
	pages         webclient.Pages
	robots        []byte
	sitemap       []byte
	manifest      webclient.Manifest
	requestCounts *logging.RequestCounts
	pace          pace
	handler       http.Handler
}

func New(settings *config.Server, storage store.Store, blobs store.Blobs, options ...Option) (*Server, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if err := abuse.RefuseLegacyReports(settings.DataDir); err != nil {
		return nil, err
	}
	ring, err := pepper.NewRing(settings.Pepper, settings.PepperPrevious)
	if err != nil {
		return nil, err
	}
	webFiles, err := embeddedWebFiles()
	if err != nil {
		return nil, err
	}
	server := &Server{
		settings:    *settings,
		store:       storage,
		blobs:       blobs,
		ring:        ring,
		leaseTimers: newReleases(),
		logger:      logging.New(os.Stderr),
		webFiles:    webFiles,
		dummyRow:    newDummyRow(),
		pace:        defaultPace(),
	}
	for _, option := range options {
		option(server)
	}
	if loadErr := server.loadWebClient(); loadErr != nil {
		return nil, loadErr
	}
	server.requestCounts = logging.NewRequestCounts(server.logger)
	server.setUpLimits()
	server.challenges = proof.NewChallenges(ring, storage)
	server.work = proof.NewWork(ring, storage, settings.PoWConcurrency, settings.PoWDifficulty)
	server.idTokens = proof.NewIDTokens(ring)
	server.reports = abuse.NewReports(
		settings.DataDir,
		settings.ReportKey,
		server.logger,
		abuse.FilesBytes(settings.Rates.GlobalDailyCreates, store.ReportsPerMessage),
	)
	server.disk = diskspace.NewReserver(settings.DataDir, settings.MinFreeBytes, diskspace.Free, server.logger)
	server.handler = server.routes()
	return server, nil
}

func (s *Server) Close() {
	s.leaseTimers.stop()
}

func (s *Server) HTTPServer() *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{
		Addr:              s.settings.Listen,
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		Protocols:         &protocols,
		HTTP2: &http.HTTP2Config{
			MaxReceiveBufferPerConnection: h2cConnectionBuffer,
			MaxReceiveBufferPerStream:     h2cStreamBuffer,
			MaxReadFrameSize:              h2cMaxFrame,
		},
		ErrorLog: logging.NewHTTPErrorLog(s.logger),
	}
}

func (s *Server) RunSweeper(ctx context.Context) {
	if s.settings.SweepInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.settings.SweepInterval)
	defer ticker.Stop()
	for {
		report, err := s.sweepOnce(ctx)
		switch {
		case ctx.Err() != nil:
			s.logger.InfoContext(ctx, "sweep-stopped", report.LogAttrs()...)
			return
		case err != nil:
			s.logger.ErrorContext(ctx, "sweep", sweep.LogAttributes(report, err)...)
		default:
			s.logger.InfoContext(ctx, "sweep", report.LogAttrs()...)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunHousekeeping works at every minute boundary: it writes the request counts of the minute, drops rate-limit
// entries that no longer limit anything and draws a new address key when a UTC day begins (spec §5.3, §10).
func (s *Server) RunHousekeeping(ctx context.Context) {
	day := ratelimit.Day(time.Now()).Start
	for {
		now := time.Now()
		select {
		case <-ctx.Done():
			s.requestCounts.Flush()
			return
		case <-time.After(now.Truncate(time.Minute).Add(time.Minute).Sub(now)):
		}
		now = time.Now()
		s.requestCounts.Flush()
		if today := ratelimit.Day(now).Start; !today.Equal(day) {
			day = today
			s.addressKey.Rotate()
			s.forgetAddresses()
		} else {
			s.purgeRateTables(now)
		}
		eraseUnreachableSecrets()
	}
}

// eraseUnreachableSecrets runs a collection, which is when secret mode erases what an ended request allocated; an
// idle server would otherwise wait up to two minutes for the runtime's forced collection.
func eraseUnreachableSecrets() {
	runtime.GC()
}

func (s *Server) purgeRateTables(now time.Time) {
	s.sourceLimiter.Purge(now)
	s.rangeLimiter.Purge(now)
	s.counters.Purge(now)
}

func (s *Server) forgetAddresses() {
	s.sourceLimiter.Reset()
	s.rangeLimiter.Reset()
	s.counters.Reset()
}

func (s *Server) sweepOnce(ctx context.Context) (store.SweepReport, error) {
	return sweep.Run(ctx, s.store, s.blobs, s.settings.DataDir, time.Now())
}

func (s *Server) setUpLimits() {
	rates := s.settings.Rates
	s.instance = ratelimit.NewFair(rates.BucketPerSecond, rates.BucketBurst)
	s.sourceLimiter = ratelimit.NewLimiter(rates.SourcePerSecond, rates.SourceBurst)
	s.rangeLimiter = ratelimit.NewLimiter(rates.BucketPerSecond*rangeShare, max(1, rates.BucketBurst*rangeShare))
	s.addressKey = ratelimit.NewAddressKey()
	s.proxies = ratelimit.Proxies{
		Trusted:    s.settings.TrustedProxies,
		ReadRealIP: s.settings.ProxyMode == config.ProxyModeScaleway,
	}
	s.counters = ratelimit.NewCounters()
	s.streams = ratelimit.NewStreams(s.settings.MaxStreams)
}
