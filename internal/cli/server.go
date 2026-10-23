package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var (
	errSweepFailed       = errors.New("sweep failed; see the sweep-once log line")
	errNoStore           = errors.New("sweep-once found no store in NYLZO_DATA_DIR")
	errServerUnsupported = errors.New("the server runs on Linux and macOS only; on Windows use WSL or Docker")
)

const (
	dbFile             = "nylzo.db"
	shutdownTimeout    = 30 * time.Second
	defaultMemoryLimit = 192 << 20
)

func runServe(ctx context.Context, env Env, args []string) int {
	return runServer(env, args, "serve", func(logger *slog.Logger) error { return serve(ctx, env, logger) })
}

func runSweepOnce(_ context.Context, env Env, args []string) int {
	return runServer(env, args, "sweep-once", func(logger *slog.Logger) error { return sweepOnce(env.Getenv, logger) })
}

func runServer(env Env, args []string, name string, run func(*slog.Logger) error) int {
	fs := newFlags(env, name)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "usage: %s %s\n\nThe server takes no flags; it reads NYLZO_* environment variables, described in README.md.\n", program, name)
	}
	if code, done := flagsDone(parseFlags(fs, env, args)); done {
		return code
	}
	logger := server.NewLogger(env.Stderr)
	err := run(logger)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errSweepFailed):
	case errors.Is(err, server.ErrConfig), errors.Is(err, errServerUnsupported):
		logger.Error(program, "err", err.Error())
	default:
		logger.Error(program, "err", server.ErrorText(err))
	}
	return exitError
}

func buildVersion(version string) string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
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

func serveConfig(env Env) (server.Config, error) {
	cfg, err := server.FromEnv(env.Getenv, true)
	if err != nil {
		return cfg, err
	}
	applyMemoryLimit(env.Getenv)
	cfg.Version = buildVersion(env.Version)
	return cfg, nil
}

func serve(ctx context.Context, env Env, logger *slog.Logger) error {
	if !serverSupported {
		return errServerUnsupported
	}
	cfg, err := serveConfig(env)
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
	ctx, stop := context.WithCancel(ctx)
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

func sweepOnce(getenv func(string) string, logger *slog.Logger) error {
	if !serverSupported {
		return errServerUnsupported
	}
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
