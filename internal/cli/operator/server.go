package operator

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

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/datadir"
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
	failureMessage     = command.Program
)

var (
	errMemoryLock = errors.New(
		"the server must lock its memory so no key reaches swap: raise the memlock limit " +
			"(systemd LimitMEMLOCK=infinity, docker run --ulimit memlock=-1:-1, Compose ulimits memlock -1) " +
			"or grant CAP_IPC_LOCK; NYLZO_ALLOW_SWAP=1 serves without the lock",
	)
	errSweepFailed       = errors.New("sweep failed; see the sweep-once log line")
	errNoStore           = errors.New("found no store in NYLZO_DATA_DIR")
	errServerUnsupported = errors.New("the server runs on Linux and macOS only; on Windows use WSL or Docker")
)

func RunServe(ctx context.Context, environment *command.Environment, arguments []string) int {
	return runServer(
		environment,
		arguments,
		"serve",
		func(logger *slog.Logger) error { return serve(ctx, environment, logger) },
	)
}

func RunSweepOnce(_ context.Context, environment *command.Environment, arguments []string) int {
	return runServer(
		environment,
		arguments,
		"sweep-once",
		func(logger *slog.Logger) error { return sweepOnce(environment.Getenv, logger) },
	)
}

func runServer(environment *command.Environment, arguments []string, name string, run func(*slog.Logger) error) int {
	flags := command.NewFlags(environment, name)
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s %s\n\nThe server takes no flags; it reads NYLZO_* environment variables, described in README.md.\n",
			command.Program,
			name,
		)
	}
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	logger := logging.New(environment.Stderr)
	err := hardenProcess()
	if err == nil {
		err = run(logger)
	}
	switch {
	case err == nil:
		return command.ExitOK
	case errors.Is(err, errSweepFailed):
	case errors.Is(err, config.ErrInvalid), errors.Is(err, errServerUnsupported), errors.Is(err, errMemoryLock),
		errors.Is(err, erase.ErrNotInPlace):
		logger.Error(failureMessage, "err", err.Error())
	default:
		logger.Error(failureMessage, "err", logging.ErrorText(err))
	}
	return command.ExitError
}

// requireMemoryLock refuses to serve with pages that the kernel may write to swap, unless swap is explicitly allowed.
func requireMemoryLock(development config.Development, logger *slog.Logger) error {
	if development.AllowSwap {
		logger.Warn("memory-not-locked", "reason", "NYLZO_ALLOW_SWAP")
		return nil
	}
	if err := lockMemory(); err != nil {
		return fmt.Errorf("%w: %w", errMemoryLock, err)
	}
	return nil
}

// requireErasable refuses a data directory on a file system that keeps old blocks, which would undo every deletion.
func requireErasable(directories []string, development config.Development, logger *slog.Logger) error {
	for _, directory := range directories {
		err := erase.RequireInPlace(directory)
		switch {
		case err == nil:
		case errors.Is(err, erase.ErrNotInPlace) && development.AllowUnerasableStorage:
			logger.Warn("storage-not-erasable", "reason", err.Error())
		case errors.Is(err, erase.ErrNotInPlace):
			return fmt.Errorf("%w (set NYLZO_ALLOW_UNERASABLE_STORAGE=1 only on a development machine)", err)
		default:
			return err
		}
	}
	return nil
}

func openStore(
	dataDir string,
	development config.Development,
	open func(context.Context, string) (*sqlite.Store, error),
	logger *slog.Logger,
) (*sqlite.Store, *disk.Blobs, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, err
	}
	if err := requireErasable([]string{dataDir}, development, logger); err != nil {
		return nil, nil, err
	}
	databasePath := filepath.Join(dataDir, databaseFile)
	narrowed, err := datadir.Secure(datadir.Layout{
		Directory: dataDir,
		Files:     []string{databasePath, databasePath + "-wal", databasePath + "-shm"},
		Trees:     []string{filepath.Join(dataDir, disk.Directory), abuse.Directory(dataDir)},
	})
	if narrowed > 0 {
		logger.Warn("permissions-narrowed", "entries", narrowed)
	}
	if err != nil {
		return nil, nil, err
	}
	database, err := open(context.Background(), databasePath)
	if err != nil {
		return nil, nil, err
	}
	blobs, err := disk.New(dataDir)
	if err == nil {
		err = requireErasable(
			[]string{filepath.Join(dataDir, disk.Directory), filepath.Join(dataDir, disk.Directory, "tmp")},
			development,
			logger,
		)
	}
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

func serveSettings(environment *command.Environment) (config.Server, error) {
	settings, err := config.FromEnv(environment.Getenv, true)
	if err != nil {
		return settings, err
	}
	if environment.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(defaultMemoryLimit)
	}
	settings.Build = environment.BuildInformation()
	settings.Version = settings.Build.Version
	return settings, nil
}

func serve(ctx context.Context, environment *command.Environment, logger *slog.Logger) error {
	if !serverSupported {
		return errServerUnsupported
	}
	settings, err := serveSettings(environment)
	if err != nil {
		return err
	}
	if lockErr := requireMemoryLock(settings.Development, logger); lockErr != nil {
		return lockErr
	}
	database, blobs, err := openStore(settings.DataDir, settings.Development, sqlite.Open, logger)
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
	if address, ok := listener.Addr().(*net.TCPAddr); !ok || !address.IP.IsLoopback() {
		logger.WarnContext(ctx, "plain-http-listener", "addr", listener.Addr().String())
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var sweeper sync.WaitGroup
	sweeper.Go(func() { instance.RunSweeper(ctx) })
	sweeper.Go(func() { instance.RunHousekeeping(ctx) })
	httpServer := instance.HTTPServer()
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(server.SecureEarlyErrors(listener)) }()
	logger.InfoContext(
		ctx,
		"listening",
		"addr",
		listener.Addr().String(),
		"version",
		settings.Version,
		"commit",
		settings.Build.Commit,
		"build_date",
		settings.Build.Date,
		"dirty",
		settings.Build.Dirty,
	)
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
	database, blobs, err := openStore(settings.DataDir, settings.Development, sqlite.OpenCurrent, logger)
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

func RunTakedown(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "takedown")
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s takedown < ids\n\nRemoves every message whose id stands on its own line of stdin; readers then see deleted. Run it on the server host; it reads NYLZO_DATA_DIR like sweep-once.\n",
			command.Program,
		)
	}
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if !serverSupported {
		return command.Failf(environment, "%s", errServerUnsupported)
	}
	if err := hardenProcess(); err != nil {
		return command.Failf(environment, "%s", err)
	}
	if environment.Terminal.StdinIsTerminal() {
		fmt.Fprintln(
			environment.Stderr,
			command.Program+" takedown reads the ids from a pipe or a file on stdin, one per line",
		)
		return command.ExitUsage
	}
	ids, err := readIDs(environment.Stdin)
	if err != nil {
		fmt.Fprintln(environment.Stderr, command.Program+" takedown: "+err.Error())
		return command.ExitUsage
	}
	settings, err := config.FromEnv(environment.Getenv, false)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	if err = requireStore(settings.DataDir); err != nil {
		return command.Failf(environment, "%s", err)
	}
	database, blobs, err := openStore(
		settings.DataDir,
		settings.Development,
		sqlite.OpenCurrent,
		slog.New(slog.NewTextHandler(environment.Stderr, nil)),
	)
	if err != nil {
		return command.Failf(environment, "%s", logging.ErrorText(err))
	}
	defer database.Close()
	return takeDown(ctx, environment, database, blobs, ids)
}

// takeDown removes each message without opening its record; readers then see deleted (D63, D101).
func takeDown(
	ctx context.Context,
	environment *command.Environment,
	database store.Store,
	blobs store.Blobs,
	ids []core.ID,
) int {
	failed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			fmt.Fprintln(environment.Stderr, command.Program+": takedown stopped before every id was handled")
			return command.ExitInterrupted
		}
		removal, err := store.TakeDown(ctx, database, blobs, seal.LocatorOf(id[:]), time.Now())
		switch {
		case errors.Is(err, store.ErrNotFound):
			fmt.Fprintf(environment.Stdout, "%s no message\n", id)
		case err != nil:
			failed++
			fmt.Fprintf(environment.Stdout, "%s failed: %s\n", id, logging.ErrorText(err))
		case removal == store.AlreadyEnded:
			fmt.Fprintf(environment.Stdout, "%s already ended\n", id)
		default:
			fmt.Fprintf(environment.Stdout, "%s deleted\n", id)
		}
	}
	if failed > 0 {
		return command.Failf(
			environment,
			"%d of %d messages could not be removed; run takedown again for them",
			failed,
			len(ids),
		)
	}
	return command.ExitOK
}

// readIDs refuses the whole input when one line is not an id, so a pasted link never ends anything.
func readIDs(reader io.Reader) ([]core.ID, error) {
	data, err := command.ReadLimited(reader, maxTakedownInput)
	if err != nil {
		return nil, errors.New("give at most 1 MiB of ids on stdin")
	}
	seen := map[string]bool{}
	var ids []core.ID
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
			ids = append(ids, id)
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
