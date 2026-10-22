package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var version = "dev"

var errSweepFailed = errors.New("sweep failed; see the sweep-once log line")

const (
	dbFile             = "nylzo.db"
	shutdownTimeout    = 30 * time.Second
	defaultMemoryLimit = 192 << 20
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	var err error
	switch cmd {
	case "serve":
		err = serve(logger)
	case "sweep-once":
		err = sweepOnce(logger)
	case "version":
		fmt.Println(version)
	default:
		err = fmt.Errorf("unknown command %q (use serve, sweep-once or version)", cmd)
	}
	if err != nil {
		logger.Error("nylzod", "err", server.ErrorText(err))
		os.Exit(1)
	}
}

func open(cfg server.Config) (*sqlite.Store, *disk.Blobs, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, nil, err
	}
	st, err := sqlite.Open(filepath.Join(cfg.DataDir, dbFile))
	if err != nil {
		return nil, nil, err
	}
	blobs, err := disk.New(cfg.DataDir)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return st, blobs, nil
}

func requireStore(dataDir string) error {
	dir, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	for _, name := range []string{dbFile, disk.Dir} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("sweep-once found no store: %w", err)
		}
	}
	return nil
}

func applyMemoryLimit(getenv func(string) string) {
	if getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(defaultMemoryLimit)
	}
}

func serveConfig(getenv func(string) string) (server.Config, error) {
	cfg, err := server.FromEnv(getenv, true)
	if err != nil {
		return cfg, err
	}
	applyMemoryLimit(getenv)
	cfg.Version = version
	return cfg, nil
}

func serve(logger *slog.Logger) error {
	cfg, err := serveConfig(os.Getenv)
	if err != nil {
		return err
	}
	st, blobs, err := open(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	srv, err := server.New(cfg, st, blobs, server.WithLogger(logger))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	var sweeper sync.WaitGroup
	sweeper.Go(func() { srv.RunSweeper(ctx) })
	defer func() {
		stop()
		sweeper.Wait()
	}()
	httpServer := srv.HTTPServer()
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	logger.Info("listening", "addr", cfg.Listen, "version", version)
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	return shutdown(httpServer, shutdownTimeout, logger)
}

func shutdown(hs *http.Server, timeout time.Duration, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := hs.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		logger.Warn("shutdown-timeout", "after", timeout.String())
		return hs.Close()
	}
	return err
}

func sweepOnce(logger *slog.Logger) error {
	cfg, err := server.FromEnv(os.Getenv, false)
	if err != nil {
		return err
	}
	if err := requireStore(cfg.DataDir); err != nil {
		return err
	}
	st, blobs, err := open(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	report, err := server.SweepOnce(context.Background(), st, blobs, time.Now())
	if err != nil {
		logger.Error("sweep-once", server.SweepLogAttrs(report, err)...)
		return errSweepFailed
	}
	logger.Info("sweep-once", report.LogAttrs()...)
	return nil
}
