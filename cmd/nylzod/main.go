package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var version = "dev"

var (
	errSweepFailed = errors.New("sweep failed; see the sweep-once log line")
	errNoStore     = errors.New("sweep-once found no store in NYLZO_DATA_DIR")
)

const (
	dbFile             = "nylzo.db"
	shutdownTimeout    = 30 * time.Second
	defaultMemoryLimit = 192 << 20
	usage              = `usage: nylzod [serve | sweep-once | version | help]

  serve       run the server (the default)
  sweep-once  run one sweep of the store and exit
  version     print the version
  help        print this help

nylzod takes no other arguments; it reads its configuration from NYLZO_*
environment variables, described in README.md.
`
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	known := []string{"serve", "sweep-once", "version", "help", "-h", "--help"}
	if len(args) > 1 || !slices.Contains(known, cmd) {
		fmt.Fprint(stderr, usage)
		return 2
	}
	logger := server.NewLogger(stderr)
	var err error
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	case "version":
		fmt.Fprintln(stdout, buildVersion())
	case "serve":
		err = serve(logger, getenv)
	case "sweep-once":
		err = sweepOnce(logger, getenv)
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errSweepFailed):
	case errors.Is(err, server.ErrConfig):
		logger.Error("nylzod", "err", err.Error())
	default:
		logger.Error("nylzod", "err", server.ErrorText(err))
	}
	return 1
}

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func open(cfg server.Config, openStore func(context.Context, string) (*sqlite.Store, error)) (*sqlite.Store, *disk.Blobs, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, nil, err
	}
	st, err := openStore(context.Background(), filepath.Join(cfg.DataDir, dbFile))
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
			return fmt.Errorf("%w: %s", errNoStore, name)
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
	cfg.Version = buildVersion()
	return cfg, nil
}

func serve(logger *slog.Logger, getenv func(string) string) error {
	cfg, err := serveConfig(getenv)
	if err != nil {
		return err
	}
	st, blobs, err := open(cfg, sqlite.Open)
	if err != nil {
		return err
	}
	defer st.Close()
	srv, err := server.New(cfg, st, blobs, server.WithLogger(logger))
	if err != nil {
		return err
	}
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", cfg.Listen)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var sweeper sync.WaitGroup
	sweeper.Go(func() { srv.RunSweeper(ctx) })
	httpServer := srv.HTTPServer()
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(ln) }()
	logger.Info("listening", "addr", ln.Addr().String(), "version", cfg.Version)
	select {
	case err = <-errCh:
		stop()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	case <-ctx.Done():
		stop()
		logger.Info("shutdown", "drain", shutdownTimeout.String())
		err = shutdown(httpServer, shutdownTimeout, logger)
	}
	waitFor(&sweeper, shutdownTimeout, logger)
	srv.Close()
	return err
}

func waitFor(wg *sync.WaitGroup, timeout time.Duration, logger *slog.Logger) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		logger.Warn("sweep-still-running", "after", timeout.String())
	}
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

func sweepOnce(logger *slog.Logger, getenv func(string) string) error {
	cfg, err := server.FromEnv(getenv, false)
	if err != nil {
		return err
	}
	if err := requireStore(cfg.DataDir); err != nil {
		return err
	}
	st, blobs, err := open(cfg, sqlite.OpenCurrent)
	if err != nil {
		return err
	}
	defer st.Close()
	report, err := server.SweepStore(context.Background(), st, blobs, cfg.DataDir, time.Now())
	if err != nil {
		logger.Error("sweep-once", server.SweepLogAttrs(report, err)...)
		return errSweepFailed
	}
	logger.Info("sweep-once", report.LogAttrs()...)
	return nil
}
