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
	sources   *sourceLimiter
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
	s.sources = newSourceLimiter(cfg.Rates.SourcePerSecond, cfg.Rates.SourceBurst)
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
		ErrorLog:          log.New(&httpErrorSink{log: s.log, bucket: newTokenBucket(1, 20, time.Now())}, "", 0),
	}
}

type httpErrorSink struct {
	log    *slog.Logger
	bucket *tokenBucket
}

var httpErrorCategories = []struct {
	prefix string
	name   string
	level  slog.Level
}{
	{"http: Accept error", "accept", slog.LevelError},
	{"http: panic serving", "panic", slog.LevelError},
	{"http: TLS handshake error", "tls-handshake", slog.LevelWarn},
	{"http2: ", "http2", slog.LevelWarn},
	{"http: superfluous response.WriteHeader", "superfluous-write-header", slog.LevelWarn},
}

func (h *httpErrorSink) Write(p []byte) (int, error) {
	if !h.bucket.allow(time.Now()) {
		return len(p), nil
	}
	name, level := "other", slog.LevelWarn
	for _, c := range httpErrorCategories {
		if bytes.HasPrefix(p, []byte(c.prefix)) {
			name, level = c.name, c.level
			break
		}
	}
	h.log.Log(context.Background(), level, "net/http error", "category", name)
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
		orphans, rerr := store.Reconcile(ctx, s.store, s.blobs, now)
		report.OrphanedBlobs = orphans
		if rerr == nil {
			d := dayWindow(now)
			_, rerr = s.store.RateHit(ctx, []store.RateHit{{Key: reconcileKey, WindowStart: d.start, PurgeAt: d.end.Add(24 * time.Hour)}})
		}
		err = errors.Join(err, rerr)
	}
	return report, err
}

func SweepLogAttrs(report store.SweepReport, err error) []any {
	attrs := report.LogAttrs()
	if err == nil {
		return attrs
	}
	errs := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs = joined.Unwrap()
	}
	return append(attrs, "errors", len(errs), "err", errs[0].Error())
}

func (s *Server) RunSweeper(ctx context.Context) {
	if s.cfg.SweepInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report, err := s.SweepOnce(ctx)
			if err != nil {
				s.log.Error("sweep", SweepLogAttrs(report, err)...)
				continue
			}
			s.log.Info("sweep", report.LogAttrs()...)
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
