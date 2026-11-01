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
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/datadir"
	"github.com/misaon/nylzo/internal/server/epoch"
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

// requireErasable refuses storage that keeps old blocks, which would undo every deletion: a file system, a mount
// default or a chattr +j on a directory or file.
func requireErasable(paths []string, development config.Development, logger *slog.Logger) error {
	err := erase.RequireInPlace(paths...)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, erase.ErrNotInPlace) && development.AllowUnerasableStorage:
		logger.Warn("storage-not-erasable", "reason", err.Error())
		return nil
	case errors.Is(err, erase.ErrNotInPlace):
		return fmt.Errorf("%w (set NYLZO_ALLOW_UNERASABLE_STORAGE=1 only on a development machine)", err)
	default:
		return err
	}
}

// erasablePaths lists the directories and files whose overwrites must land in place: the data directory, the
// database files and every blob and temporary upload.
func erasablePaths(dataDir string) ([]string, error) {
	databasePath := filepath.Join(dataDir, databaseFile)
	blobDirectory := filepath.Join(dataDir, disk.Directory)
	temporaryDirectory := filepath.Join(blobDirectory, "tmp")
	paths := append([]string{dataDir, blobDirectory, temporaryDirectory}, databaseFiles(databasePath)...)
	for _, directory := range []string{blobDirectory, temporaryDirectory} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() {
				paths = append(paths, filepath.Join(directory, entry.Name()))
			}
		}
	}
	return paths, nil
}

// databaseFiles lists the files of the database that exist; the store checks them again once it has opened.
func databaseFiles(databasePath string) []string {
	var paths []string
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if _, err := os.Lstat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
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
	blobs, err := disk.New(dataDir)
	var paths []string
	if err == nil {
		paths, err = erasablePaths(dataDir)
	}
	if err == nil {
		err = requireErasable(paths, development, logger)
	}
	if err != nil {
		return nil, nil, err
	}
	database, err := open(context.Background(), databasePath)
	if err != nil {
		return nil, nil, err
	}
	if err = requireErasable(databaseFiles(databasePath), development, logger); err != nil {
		database.Close()
		return nil, nil, err
	}
	return database, blobs, nil
}

// prepareEpoch checks the epoch directory before the store opens, so a refusal comes before an upgrade erases.
func prepareEpoch(settings *config.Server, logger *slog.Logger) error {
	if err := server.PrepareEpochDirectory(settings); err != nil {
		return err
	}
	directory := settings.EpochDirectory()
	if _, err := epoch.Inspect(directory); err != nil {
		return err
	}
	return requireErasable([]string{directory}, settings.Development, logger)
}

// upgradingOpen erases a data directory of nylzo 0.1 or 0.2 before it opens it, because protocol version 2 reads
// none of its messages; an interrupted upgrade finishes on the next start.
func upgradingOpen(dataDir string, logger *slog.Logger) func(context.Context, string) (*sqlite.Store, error) {
	return func(ctx context.Context, path string) (*sqlite.Store, error) {
		upgraded, err := sqlite.UpgradeFromReleased(ctx, path, func() error { return disk.EraseAll(dataDir) })
		if err != nil {
			return nil, err
		}
		if upgraded {
			logger.WarnContext(
				ctx,
				"store-upgraded",
				"notice", "protocol version 2: every message stored before this upgrade was erased",
			)
		}
		return sqlite.Open(ctx, path)
	}
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
	if epochErr := prepareEpoch(&settings, logger); epochErr != nil {
		return epochErr
	}
	database, blobs, err := openStore(
		settings.DataDir,
		settings.Development,
		upgradingOpen(settings.DataDir, logger),
		logger,
	)
	if err != nil {
		return err
	}
	defer database.Close()
	database.OnScrubFailure(func(err error) { logger.ErrorContext(ctx, "wal-scrub", "err", logging.ErrorText(err)) })
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
	sweeper.Go(func() { instance.RunRatchet(ctx) })
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
	now := time.Now()
	report, err := sweep.Run(context.Background(), database, blobs, settings.DataDir, now)
	err = errors.Join(err, advanceRatchet(&settings, now, logger))
	if err != nil {
		logger.Error("sweep-once", sweep.LogAttributes(report, err)...)
		return errSweepFailed
	}
	logger.Info("sweep-once", report.LogAttrs()...)
	return nil
}

// advanceRatchet erases the epoch keys of the hours that have passed while serve does not run; it starts no ratchet,
// and only a directory the operator named must exist.
func advanceRatchet(settings *config.Server, now time.Time, logger *slog.Logger) error {
	options := epoch.Options{CheckFileSystem: func(directory string) error {
		return requireErasable([]string{directory}, settings.Development, logger)
	}}
	var ratchet *epoch.Ratchet
	var err error
	secretmode.Do(func() { ratchet, _, err = epoch.Open(settings.EpochDirectory(), now, options) })
	if ratchet != nil {
		ratchet.Close()
	}
	if errors.Is(err, epoch.ErrNoDirectory) && settings.EpochDir == "" {
		return nil
	}
	return err
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
