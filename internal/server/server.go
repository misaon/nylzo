package server

import (
	"bytes"
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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
	httpErrorLogRate  = 1
	httpErrorLogBurst = 20
	reconcileKey      = "sweep:reconcile:d"
)

type Option func(*Server)

func WithClock(clock func() time.Time) Option {
	return func(s *Server) { s.clock = clock }
}

func WithAfterFunc(after func(d time.Duration, fn func())) Option {
	return func(s *Server) { s.afterFunc = after }
}

func WithLogger(l *slog.Logger) Option {
	return func(s *Server) { s.log = l }
}

type Server struct {
	cfg       Config
	store     store.Store
	blobs     store.Blobs
	ring      *pepper.Ring
	clock     func() time.Time
	afterFunc func(time.Duration, func())
	log       *slog.Logger
	bucket    *tokenBucket
	sources   *keyedLimiter
	prefixes  *keyedLimiter
	powSlots  chan struct{}
	handler   http.Handler
	abuseMu   sync.Mutex
	abusePath string
}

func New(cfg Config, st store.Store, blobs store.Blobs, opts ...Option) (*Server, error) {
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
		abusePath: filepath.Join(cfg.DataDir, "abuse-reports.jsonl"),
	}
	for _, o := range opts {
		o(s)
	}
	s.bucket = newTokenBucket(cfg.Rates.BucketPerSecond, cfg.Rates.BucketBurst, s.clock())
	s.sources = newKeyedLimiter(cfg.Rates.SourcePerSecond, cfg.Rates.SourceBurst)
	s.prefixes = newKeyedLimiter(cfg.Rates.BucketPerSecond*prefixShare, max(1, cfg.Rates.BucketBurst*prefixShare))
	s.powSlots = make(chan struct{}, cfg.PoWConcurrency)
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
		ErrorLog:          log.New(newHTTPErrorSink(s.log, s.clock), "", 0),
	}
}

type sinkLevel struct {
	bucket  *tokenBucket
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
	now := clock()
	return &httpErrorSink{
		log:   l,
		clock: clock,
		warn:  sinkLevel{bucket: newTokenBucket(httpErrorLogRate, httpErrorLogBurst, now)},
		err:   sinkLevel{bucket: newTokenBucket(httpErrorLogRate, httpErrorLogBurst, now)},
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
	if !sink.bucket.allow(h.clock()) {
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
	now := s.clock()
	report, err := store.SweepAll(ctx, s.store, s.blobs, now)
	done, derr := s.store.RateGet(ctx, reconcileKey, dayWindow(now).start)
	if derr != nil {
		return report, errors.Join(err, derr)
	}
	if done.Count == 0 {
		orphans, listed, rerr := store.Reconcile(ctx, s.store, s.blobs, now)
		report.OrphanedBlobs = orphans
		if listed {
			d := dayWindow(now)
			if _, merr := s.store.RateHit(ctx, []store.RateHit{{Key: reconcileKey, WindowStart: d.start, PurgeAt: d.purgeAt()}}); merr != nil {
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
	return append(attrs, "errors", len(leaves), "err", leaves[0].Error())
}

func (s *Server) RunSweeper(ctx context.Context) {
	if s.cfg.SweepInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		report, err := s.SweepOnce(ctx)
		if err != nil {
			s.log.Error("sweep", SweepLogAttrs(report, err)...)
		} else {
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
	mux.HandleFunc("POST /api/v1/messages", s.handleCreate)
	mux.HandleFunc("GET /api/v1/messages/{id}", s.handleMetadata)
	mux.HandleFunc("POST /api/v1/messages/{id}/claim", s.handleClaim)
	mux.HandleFunc("GET /api/v1/messages/{id}/blob", s.handleBlob)
	mux.HandleFunc("POST /api/v1/messages/{id}/ack", s.handleAck)
	mux.HandleFunc("POST /api/v1/messages/{id}/burn", s.handleBurn)
	mux.HandleFunc("POST /api/v1/messages/{id}/cancel", s.handleCancel)
	mux.HandleFunc("GET /api/v1/messages/{id}/status", s.handleStatus)
	mux.HandleFunc("POST /api/v1/abuse", s.handleAbuse)
	mux.HandleFunc("/api/", s.handleAPINotFound)
	mux.HandleFunc("GET /m/{id}", s.handlePage)
	mux.HandleFunc("GET /{$}", s.handlePage)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return s.logging(s.securityHeaders(s.prefetchGuard(s.throttle(mux))))
}

func NewSweeper(cfg Config, st store.Store, blobs store.Blobs, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &Server{cfg: cfg, store: st, blobs: blobs, clock: time.Now, log: log}
}
