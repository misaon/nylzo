package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 120 * time.Second
	writeTimeout      = 120 * time.Second
	idleTimeout       = 30 * time.Second
	maxHeaderBytes    = 8 << 10
	h2cStreamBuffer   = 64 << 10
	h2cConnBuffer     = 1 << 20
	h2cMaxFrame       = 16 << 10
	streamBusyRetry   = 5 * time.Second
	diskFullRetry     = 60 * time.Second
	reconcileKey      = "sweep:reconcile:d"
)

type Option func(*Server)

func WithClock(clock func() time.Time) Option {
	return func(s *Server) {
		if clock != nil {
			s.clock = clock
		}
	}
}

func WithAfterFunc(after func(d time.Duration, fn func())) Option {
	return func(s *Server) {
		if after != nil {
			s.afterFunc = after
		}
	}
}

func WithLogger(l *slog.Logger) Option {
	return func(s *Server) {
		if l != nil {
			s.log = l
		}
	}
}

type Server struct {
	cfg           Config
	store         store.Store
	blobs         store.Blobs
	ring          *pepper.Ring
	clock         func() time.Time
	afterFunc     func(time.Duration, func())
	timers        *timers
	log           *slog.Logger
	instance      *ratelimit.Limiter
	sources       *ratelimit.Limiter
	prefixes      *ratelimit.Limiter
	addressKey    *ratelimit.AddressKey
	proxies       ratelimit.Proxies
	counters      *ratelimit.Counters
	forwardNotice atomic.Int64
	spent         *spentChallenges
	spends        *spendBatcher
	powSlots      chan struct{}
	powSlotWait   time.Duration
	streams       *ratelimit.Streams
	freeSpace     func(string) (uint64, error)
	diskLow       atomic.Bool
	diskReserved  atomic.Int64
	handler       http.Handler
	abuseMu       sync.Mutex
	abuseDir      string
	webFiles      fs.FS
	web           webClient
	pages         map[string]page
	robots        []byte
	sitemap       []byte
	manifest      ClientManifest
}

func New(cfg Config, st store.Store, blobs store.Blobs, opts ...Option) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ring, err := pepper.NewRing(cfg.Pepper, cfg.PepperPrevious)
	if err != nil {
		return nil, err
	}
	pending := &timers{}
	s := &Server{
		cfg:       cfg,
		store:     st,
		blobs:     blobs,
		ring:      ring,
		clock:     time.Now,
		afterFunc: pending.after,
		timers:    pending,
		log:       logging.New(os.Stderr),
		abuseDir:  abuseDir(cfg.DataDir),
		webFiles:  webFiles,
		freeSpace: freeSpace,
	}
	for _, o := range opts {
		o(s)
	}
	if s.web, err = loadWebClient(s.webFiles); err != nil {
		return nil, err
	}
	if s.pages, err = renderPages(s.web, cfg.PublicURL); err != nil {
		return nil, err
	}
	s.log.Info("web-client", "built", s.web.built, "assets", len(s.web.assets), "public_url", cfg.PublicURL)
	s.robots = robotsTxt(cfg.PublicURL)
	if cfg.PublicURL != "" {
		s.sitemap = sitemapXML(cfg.PublicURL)
	}
	s.manifest = s.web.embedded().Manifest(cfg.Version, cfg.PublicURL)
	s.instance = ratelimit.NewLimiter(cfg.Rates.BucketPerSecond, cfg.Rates.BucketBurst)
	s.sources = ratelimit.NewLimiter(cfg.Rates.SourcePerSecond, cfg.Rates.SourceBurst)
	s.prefixes = ratelimit.NewLimiter(cfg.Rates.BucketPerSecond*prefixShare, max(1, cfg.Rates.BucketBurst*prefixShare))
	s.addressKey = ratelimit.NewAddressKey()
	s.proxies = ratelimit.Proxies{Trusted: cfg.TrustedProxies, ReadRealIP: cfg.ProxyMode == ProxyModeScaleway}
	s.counters = ratelimit.NewCounters()
	s.spent = newSpentChallenges()
	s.spends = newSpendBatcher(st)
	s.powSlots = make(chan struct{}, cfg.PoWConcurrency)
	s.powSlotWait = powSlotWait
	s.streams = ratelimit.NewStreams(cfg.MaxStreams)
	s.handler = s.routes()
	return s, nil
}

func (s *Server) Close() {
	s.timers.stop()
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) HTTPServer() *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		Protocols:         &protocols,
		HTTP2: &http.HTTP2Config{
			MaxReceiveBufferPerConnection: h2cConnBuffer,
			MaxReceiveBufferPerStream:     h2cStreamBuffer,
			MaxReadFrameSize:              h2cMaxFrame,
		},
		ErrorLog: logging.NewHTTPErrorLog(s.log, s.clock),
	}
}

func (s *Server) SweepOnce(ctx context.Context) (store.SweepReport, error) {
	return SweepStore(ctx, s.store, s.blobs, s.cfg.DataDir, s.clock())
}

func SweepStore(
	ctx context.Context,
	st store.Store,
	blobs store.Blobs,
	dataDir string,
	now time.Time,
) (store.SweepReport, error) {
	report, err := store.SweepAll(ctx, st, blobs, now)
	err = errors.Join(err, pruneAbuse(abuseDir(dataDir), now))
	done, derr := st.RateGet(ctx, reconcileKey, ratelimit.Day(now).Start)
	if derr != nil {
		return report, errors.Join(err, derr)
	}
	if done.Count == 0 {
		orphans, listed, rerr := store.Reconcile(ctx, st, blobs, now)
		report.OrphanedBlobs = orphans
		if listed {
			d := ratelimit.Day(now)
			if _, merr := st.RateHit(
				ctx,
				[]store.RateHit{{Key: reconcileKey, WindowStart: d.Start, PurgeAt: d.PurgeAt()}},
			); merr != nil {
				rerr = errors.Join(rerr, merr)
			}
		}
		err = errors.Join(err, rerr)
	}
	return report, err
}

func leafErrors(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, e := range joined.Unwrap() {
			out = append(out, leafErrors(e)...)
		}
		return out
	}
	return []error{err}
}

func SweepLogAttrs(report store.SweepReport, err error) []any {
	attrs := report.LogAttrs()
	if err == nil {
		return attrs
	}
	leaves := leafErrors(err)
	if len(leaves) == 0 {
		leaves = []error{err}
	}
	return append(attrs, "errors", len(leaves), "err", logging.ErrorText(leaves[0]))
}

func (s *Server) RunSweeper(ctx context.Context) {
	if s.cfg.SweepInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		report, err := s.SweepOnce(ctx)
		switch {
		case ctx.Err() != nil:
			s.log.Info("sweep-stopped", report.LogAttrs()...)
			return
		case err != nil:
			s.log.Error("sweep", SweepLogAttrs(report, err)...)
		default:
			s.log.Info("sweep", report.LogAttrs()...)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/instance", s.handleInstance)
	mux.HandleFunc("HEAD /api/v1/instance", s.handleAPINotFound)
	mux.HandleFunc("GET /api/v1/challenge", s.handleChallenge)
	mux.HandleFunc("HEAD /api/v1/challenge", s.handleAPINotFound)
	mux.HandleFunc("POST /api/v1/messages", s.handleCreate)
	mux.HandleFunc("GET /api/v1/messages/{id}", s.handleMetadata)
	mux.HandleFunc("HEAD /api/v1/messages/{id}", s.handleAPINotFound)
	mux.HandleFunc("POST /api/v1/messages/{id}/claim", s.handleClaim)
	mux.HandleFunc("GET /api/v1/messages/{id}/blob", s.handleBlob)
	mux.HandleFunc("HEAD /api/v1/messages/{id}/blob", s.handleAPINotFound)
	mux.HandleFunc("POST /api/v1/messages/{id}/ack", s.handleAck)
	mux.HandleFunc("POST /api/v1/messages/{id}/burn", s.handleBurn)
	mux.HandleFunc("POST /api/v1/messages/{id}/cancel", s.handleCancel)
	mux.HandleFunc("GET /api/v1/messages/{id}/status", s.handleStatus)
	mux.HandleFunc("HEAD /api/v1/messages/{id}/status", s.handleAPINotFound)
	mux.HandleFunc("POST /api/v1/abuse", s.handleAbuse)
	mux.HandleFunc("/api/", s.handleAPINotFound)
	mux.HandleFunc("GET /m/{id}", s.handlePage)
	mux.HandleFunc("GET /assets/{name}", s.handleAsset)
	mux.HandleFunc("GET /{$}", s.handlePage)
	mux.HandleFunc("GET /cs/{$}", s.handlePage)
	mux.HandleFunc("GET /cs", s.handleCzechHome)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
	mux.HandleFunc("GET /sent", s.handlePage)
	mux.HandleFunc("GET /report", s.handlePage)
	mux.HandleFunc("GET /transparency", s.handlePage)
	mux.HandleFunc("GET "+ClientManifestPath, s.handleClientManifest)
	mux.HandleFunc("GET "+TransparencyPath, s.handleTransparency)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return logging.Requests(s.log,
		s.noticeForwarded(s.securityHeaders(s.spendChallenges(s.prefetchGuard(s.throttle(logging.Route(mux)))))),
	)
}
