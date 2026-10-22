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
	"syscall"
	"time"

	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var version = "dev"

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
		err = fmt.Errorf("unknown command %q (use serve or sweep-once)", cmd)
	}
	if err != nil {
		logger.Error("nylzod", "err", err.Error())
		os.Exit(1)
	}
}

func open(cfg server.Config) (*sqlite.Store, *disk.Blobs, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, nil, err
	}
	st, err := sqlite.Open(filepath.Join(cfg.DataDir, "nylzo.db"))
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

func serve(logger *slog.Logger) error {
	cfg, err := server.FromEnv(os.Getenv, true)
	if err != nil {
		return err
	}
	cfg.Version = version
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
	defer stop()
	go srv.RunSweeper(ctx)
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func sweepOnce(logger *slog.Logger) error {
	cfg, err := server.FromEnv(os.Getenv, false)
	if err != nil {
		return err
	}
	st, blobs, err := open(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	srv, err := server.NewSweeper(cfg, st, blobs, logger)
	if err != nil {
		return err
	}
	report, err := srv.SweepOnce(context.Background())
	if err != nil {
		return err
	}
	logger.Info("sweep-once", "expired", report.Expired, "leases", report.ExpiredLeases, "released", report.BlobsReleased, "tombstones", report.Tombstones, "counters", report.Counters, "attempts", report.Attempts, "orphans", report.OrphanedBlobs, "temp", report.StaleTempFiles)
	return nil
}
