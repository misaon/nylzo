package server

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/pepper"
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
	httpErrorLogRate  = 1
	httpErrorLogBurst = 20
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
	cfg         Config
	store       store.Store
	blobs       store.Blobs
	ring        *pepper.Ring
	clock       func() time.Time
	afterFunc   func(time.Duration, func())
	log         *slog.Logger
	bucket      *keyedLimiter
	sources     *keyedLimiter
	prefixes    *keyedLimiter
	addrKey     *addrKey
	counters    *addrCounters
	spent       *spentChallenges
	spends      *spendBatcher
	powSlots    chan struct{}
	powSlotWait time.Duration
	streams     chan struct{}
	handler     http.Handler
	abuseMu     sync.Mutex
	abuseDir    string
	webFiles    fs.FS
	web         webClient
}

func New(cfg Config, st store.Store, blobs store.Blobs, opts ...Option) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ring, err := pepper.NewRing(cfg.Pepper, cfg.PepperPrevious)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:       cfg,
		store:     st,
		blobs:     blobs,
		ring:      ring,
		clock:     time.Now,
		afterFunc: func(d time.Duration, fn func()) { time.AfterFunc(d, fn) },
		log:       slog.New(slog.NewJSONHandler(os.Stderr, nil)),
		abuseDir:  abuseDir(cfg.DataDir),
		webFiles:  webFiles,
	}
	for _, o := range opts {
		o(s)
	}
	if s.web, err = loadWebClient(s.webFiles); err != nil {
		return nil, err
	}
	s.log.Info("web-client", "built", s.web.built, "assets", len(s.web.assets))
	s.bucket = newKeyedLimiter(cfg.Rates.BucketPerSecond, cfg.Rates.BucketBurst)
	s.sources = newKeyedLimiter(cfg.Rates.SourcePerSecond, cfg.Rates.SourceBurst)
	s.prefixes = newKeyedLimiter(cfg.Rates.BucketPerSecond*prefixShare, max(1, cfg.Rates.BucketBurst*prefixShare))
	s.addrKey = newAddrKey()
	s.counters = newAddrCounters()
	s.spent = newSpentChallenges()
	s.spends = newSpendBatcher(st)
	s.powSlots = make(chan struct{}, cfg.PoWConcurrency)
	s.powSlotWait = powSlotWait
	s.streams = make(chan struct{}, cfg.MaxStreams)
	s.handler = s.routes()
	return s, nil
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
		HTTP2:             &http.HTTP2Config{MaxReceiveBufferPerConnection: h2cConnBuffer, MaxReceiveBufferPerStream: h2cStreamBuffer, MaxReadFrameSize: h2cMaxFrame},
		ErrorLog:          log.New(newHTTPErrorSink(s.log, s.clock), "", 0),
	}
}

type sinkLevel struct {
	bucket  *keyedLimiter
	dropped int
}

type httpErrorSink struct {
	log   *slog.Logger
	clock func() time.Time
	mu    sync.Mutex
	warn  sinkLevel
	err   sinkLevel
}

func newHTTPErrorSink(l *slog.Logger, clock func() time.Time) *httpErrorSink {
	return &httpErrorSink{
		log:   l,
		clock: clock,
		warn:  sinkLevel{bucket: newKeyedLimiter(httpErrorLogRate, httpErrorLogBurst)},
		err:   sinkLevel{bucket: newKeyedLimiter(httpErrorLogRate, httpErrorLogBurst)},
	}
}

var httpErrorCategories = []struct {
	prefix string
	name   string
	level  slog.Level
}{
	{"http: Accept error", "accept", slog.LevelError},
	{"http: panic serving", "panic", slog.LevelError},
	{"http2: panic serving", "panic", slog.LevelError},
	{"http: TLS handshake error", "tls-handshake", slog.LevelWarn},
	{"http2: ", "http2", slog.LevelWarn},
	{"http: superfluous response.WriteHeader", "superfluous-write-header", slog.LevelWarn},
}

func (h *httpErrorSink) Write(p []byte) (int, error) {
	name, level := "other", slog.LevelWarn
	for _, c := range httpErrorCategories {
		if bytes.HasPrefix(p, []byte(c.prefix)) {
			name, level = c.name, c.level
			break
		}
	}
	sink := &h.warn
	if level >= slog.LevelError {
		sink = &h.err
	}
	h.mu.Lock()
	if !sink.bucket.allow(singleKey, h.clock()) {
		sink.dropped++
		h.mu.Unlock()
		return len(p), nil
	}
	dropped := sink.dropped
	sink.dropped = 0
	h.mu.Unlock()
	h.log.Log(context.Background(), level, "net/http error", "category", name, "dropped", dropped)
	return len(p), nil
}

func (s *Server) SweepOnce(ctx context.Context) (store.SweepReport, error) {
	return SweepStore(ctx, s.store, s.blobs, s.cfg.DataDir, s.clock())
}

func SweepStore(ctx context.Context, st store.Store, blobs store.Blobs, dataDir string, now time.Time) (store.SweepReport, error) {
	report, err := store.SweepAll(ctx, st, blobs, now)
	err = errors.Join(err, pruneAbuse(abuseDir(dataDir), now))
	done, derr := st.RateGet(ctx, reconcileKey, dayWindow(now).start)
	if derr != nil {
		return report, errors.Join(err, derr)
	}
	if done.Count == 0 {
		orphans, listed, rerr := store.Reconcile(ctx, st, blobs, now)
		report.OrphanedBlobs = orphans
		if listed {
			d := dayWindow(now)
			if _, merr := st.RateHit(ctx, []store.RateHit{{Key: reconcileKey, WindowStart: d.start, PurgeAt: d.purgeAt()}}); merr != nil {
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
	return append(attrs, "errors", len(leaves), "err", ErrorText(leaves[0]))
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
	mux.HandleFunc("GET /api/v1/challenge", s.handleChallenge)
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
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return s.logging(s.securityHeaders(s.spendChallenges(s.prefetchGuard(s.throttle(recordRoute(mux))))))
}
