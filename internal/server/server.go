package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	reconcileEvery    = 24 * time.Hour
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
	cfg         Config
	store       store.Store
	blobs       store.Blobs
	ring        *pepper.Ring
	clock       func() time.Time
	afterFunc   func(time.Duration, func())
	log         *slog.Logger
	bucket      *tokenBucket
	powSlots    chan struct{}
	globalCount atomic.Int64
	handler     http.Handler
	abuseMu     sync.Mutex
	abusePath   string
	lastRecon   atomic.Int64
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
	s.powSlots = make(chan struct{}, cfg.PoWConcurrency)
	if cfg.Rates.GlobalDailyCreates > 0 {
		if c, err := st.RateGet(context.Background(), "global:d", dayWindow(s.clock()).start); err == nil {
			s.globalCount.Store(c.Count)
		}
	}
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
	}
}

func (s *Server) SweepOnce(ctx context.Context) (store.SweepReport, error) {
	now := s.clock()
	reconcile := now.UnixNano()-s.lastRecon.Load() >= int64(reconcileEvery)
	report, err := store.SweepAll(ctx, s.store, s.blobs, now, reconcile)
	if err == nil && reconcile {
		s.lastRecon.Store(now.UnixNano())
	}
	return report, err
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
				s.log.Error("sweep", "err", err.Error())
				continue
			}
			s.log.Info("sweep", "expired", report.Expired, "leases", report.ExpiredLeases, "released", report.BlobsReleased, "tombstones", report.Tombstones, "counters", report.Counters, "attempts", report.Attempts, "orphans", report.OrphanedBlobs)
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
	mux.HandleFunc("GET /assets/{file}", s.handleAsset)
	return s.logging(s.securityHeaders(s.prefetchGuard(s.throttle(mux))))
}

func NewSweeper(cfg Config, st store.Store, blobs store.Blobs, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &Server{cfg: cfg, store: st, blobs: blobs, clock: time.Now, log: log}, nil
}
