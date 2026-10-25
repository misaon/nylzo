package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/sweep"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

// The server needs a directory fsync and free space checks, which Windows does not offer (D54).
const serverSupported = runtime.GOOS != "windows"

const (
	databaseFile       = "nylzo.db"
	shutdownTimeout    = 30 * time.Second
	defaultMemoryLimit = 192 << 20
	maxTakedownInput   = 1 << 20
	maxTakedownIDs     = 10000
)

var (
	errSweepFailed       = errors.New("sweep failed; see the sweep-once log line")
	errNoStore           = errors.New("found no store in NYLZO_DATA_DIR")
	errServerUnsupported = errors.New("the server runs on Linux and macOS only; on Windows use WSL or Docker")
)

func runServe(ctx context.Context, environment *Environment, arguments []string) int {
	return runServer(
		environment,
		arguments,
		"serve",
		func(logger *slog.Logger) error { return serve(ctx, environment, logger) },
	)
}

func runSweepOnce(_ context.Context, environment *Environment, arguments []string) int {
	return runServer(
		environment,
		arguments,
		"sweep-once",
		func(logger *slog.Logger) error { return sweepOnce(environment.Getenv, logger) },
	)
}

func runServer(environment *Environment, arguments []string, name string, run func(*slog.Logger) error) int {
	flags := newFlags(environment, name)
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s %s\n\nThe server takes no flags; it reads NYLZO_* environment variables, described in README.md.\n",
			program,
			name,
		)
	}
	if code, done := flagsDone(parseFlags(flags, environment, arguments)); done {
		return code
	}
	logger := logging.New(environment.Stderr)
	err := run(logger)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errSweepFailed):
	case errors.Is(err, config.ErrInvalid), errors.Is(err, errServerUnsupported):
		logger.Error(program, "err", err.Error())
	default:
		logger.Error(program, "err", logging.ErrorText(err))
	}
	return exitError
}

func openStore(
	dataDir string,
	open func(context.Context, string) (*sqlite.Store, error),
) (*sqlite.Store, *disk.Blobs, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, err
	}
	database, err := open(context.Background(), filepath.Join(dataDir, databaseFile))
	if err != nil {
		return nil, nil, err
	}
	blobs, err := disk.New(dataDir)
	if err != nil {
		database.Close()
		return nil, nil, err
	}
	return database, blobs, nil
}

// requireStore keeps sweep-once and takedown from creating an empty store where none was expected.
func requireStore(dataDir string) error {
	directory, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	for _, name := range []string{databaseFile, disk.Directory} {
		if _, statErr := os.Stat(filepath.Join(directory, name)); statErr != nil {
			return fmt.Errorf("%w: %s", errNoStore, name)
		}
	}
	return nil
}

func serveSettings(environment *Environment) (config.Server, error) {
	settings, err := config.FromEnv(environment.Getenv, true)
	if err != nil {
		return settings, err
	}
	if environment.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(defaultMemoryLimit)
	}
	settings.Version = buildVersion(environment.Version)
	return settings, nil
}

func serve(ctx context.Context, environment *Environment, logger *slog.Logger) error {
	if !serverSupported {
		return errServerUnsupported
	}
	settings, err := serveSettings(environment)
	if err != nil {
		return err
	}
	database, blobs, err := openStore(settings.DataDir, sqlite.Open)
	if err != nil {
		return err
	}
	defer database.Close()
	instance, err := server.New(&settings, database, blobs, server.WithLogger(logger))
	if err != nil {
		return err
	}
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", settings.Listen)
	if err != nil {
		return err
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var sweeper sync.WaitGroup
	sweeper.Go(func() { instance.RunSweeper(ctx) })
	httpServer := instance.HTTPServer()
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	logger.InfoContext(ctx, "listening", "addr", listener.Addr().String(), "version", settings.Version)
	select {
	case err = <-served:
		stop()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	case <-ctx.Done():
		stop()
		logger.InfoContext(ctx, "shutdown", "drain", shutdownTimeout.String())
		err = shutdown(httpServer, shutdownTimeout, logger)
	}
	waitFor(&sweeper, shutdownTimeout, logger)
	instance.Close()
	return err
}

func waitFor(group *sync.WaitGroup, timeout time.Duration, logger *slog.Logger) {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		logger.Warn("sweep-still-running", "after", timeout.String())
	}
}

func shutdown(httpServer *http.Server, timeout time.Duration, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := httpServer.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		logger.Warn("shutdown-timeout", "after", timeout.String())
		return httpServer.Close()
	}
	return err
}

func sweepOnce(getenv func(string) string, logger *slog.Logger) error {
	if !serverSupported {
		return errServerUnsupported
	}
	settings, err := config.FromEnv(getenv, false)
	if err != nil {
		return err
	}
	if storeErr := requireStore(settings.DataDir); storeErr != nil {
		return storeErr
	}
	database, blobs, err := openStore(settings.DataDir, sqlite.OpenCurrent)
	if err != nil {
		return err
	}
	defer database.Close()
	report, err := sweep.Run(context.Background(), database, blobs, settings.DataDir, time.Now())
	if err != nil {
		logger.Error("sweep-once", sweep.LogAttributes(report, err)...)
		return errSweepFailed
	}
	logger.Info("sweep-once", report.LogAttrs()...)
	return nil
}

func runTakedown(ctx context.Context, environment *Environment, arguments []string) int {
	flags := newFlags(environment, "takedown")
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s takedown < ids\n\nEnds every message whose id stands on its own line of stdin, as deleted. Run it on the server host; it reads NYLZO_DATA_DIR like sweep-once.\n",
			program,
		)
	}
	if code, done := flagsDone(parseFlags(flags, environment, arguments)); done {
		return code
	}
	if !serverSupported {
		return failf(environment, "%s", errServerUnsupported)
	}
	if environment.Terminal.StdinIsTerminal() {
		fmt.Fprintln(environment.Stderr, program+" takedown reads the ids from a pipe or a file on stdin, one per line")
		return exitUsage
	}
	ids, err := readIDs(environment.Stdin)
	if err != nil {
		fmt.Fprintln(environment.Stderr, program+" takedown: "+err.Error())
		return exitUsage
	}
	settings, err := config.FromEnv(environment.Getenv, false)
	if err != nil {
		return failf(environment, "%s", err)
	}
	if err = requireStore(settings.DataDir); err != nil {
		return failf(environment, "%s", err)
	}
	database, blobs, err := openStore(settings.DataDir, sqlite.OpenCurrent)
	if err != nil {
		return failf(environment, "%s", logging.ErrorText(err))
	}
	defer database.Close()
	return takeDown(ctx, environment, database, blobs, ids)
}

// takeDown ends each message like a burn, as deleted (D63).
func takeDown(
	ctx context.Context,
	environment *Environment,
	database store.Store,
	blobs store.Blobs,
	ids []string,
) int {
	failed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			fmt.Fprintln(environment.Stderr, program+": takedown stopped before every id was handled")
			return exitInterrupted
		}
		final, err := store.EndMessage(ctx, database, blobs, id, store.StateDeleted, time.Now())
		switch {
		case errors.Is(err, store.ErrNotFound):
			fmt.Fprintf(environment.Stdout, "%s no live message\n", id)
		case err != nil:
			failed++
			fmt.Fprintf(environment.Stdout, "%s failed: %s\n", id, logging.ErrorText(err))
		case final != store.StateDeleted:
			fmt.Fprintf(environment.Stdout, "%s already ended as %s\n", id, final)
		default:
			fmt.Fprintf(environment.Stdout, "%s deleted\n", id)
		}
	}
	if failed > 0 {
		return failf(environment, "%d of %d messages could not be ended; run takedown again for them", failed, len(ids))
	}
	return exitOK
}

// readIDs refuses the whole input when one line is not an id, so a pasted link never ends anything.
func readIDs(reader io.Reader) ([]string, error) {
	data, err := readLimited(reader, maxTakedownInput)
	if err != nil {
		return nil, errors.New("give at most 1 MiB of ids on stdin")
	}
	seen := map[string]bool{}
	var ids []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		id, parseErr := core.ParseID(text)
		if parseErr != nil {
			return nil, fmt.Errorf("line %d is not a message id; give one id per line, never a link", line)
		}
		if !seen[id.String()] {
			seen[id.String()] = true
			ids = append(ids, id.String())
		}
	}
	if scanner.Err() != nil {
		return nil, errors.New("a line on stdin is too long to be a message id")
	}
	if len(ids) == 0 {
		return nil, errors.New("give the ids to end on stdin, one per line")
	}
	if len(ids) > maxTakedownIDs {
		return nil, fmt.Errorf("give at most %d ids at once", maxTakedownIDs)
	}
	return ids, nil
}
