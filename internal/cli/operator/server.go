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
	errSecretMode = errors.New(
		"this build leaves request keys in memory after use: build with GOEXPERIMENT=runtimesecret for " +
			"linux/amd64 or linux/arm64; NYLZO_ALLOW_UNERASED_MEMORY=1 serves without erasing them",
	)
	errSweepFailed       = errors.New("sweep failed; see the sweep-once log line")
	errNoStore           = errors.New("found no store in NYLZO_DATA_DIR")
	errServerUnsupported = errors.New("the server runs on Linux and macOS only; on Windows use WSL or Docker")
)

func RunServe(ctx context.Context, environment *command.Environment, arguments []string) int {
	return runServer(ctx, environment, arguments, "serve", func(ctx context.Context, logger *slog.Logger) error {
		return serve(ctx, environment, logger)
	})
}

func RunSweepOnce(ctx context.Context, environment *command.Environment, arguments []string) int {
	return runServer(ctx, environment, arguments, "sweep-once", func(ctx context.Context, logger *slog.Logger) error {
		return sweepOnce(ctx, environment.Getenv, logger)
	})
}

func runServer(
	ctx context.Context,
	environment *command.Environment,
	arguments []string,
	name string,
	run func(context.Context, *slog.Logger) error,
) int {
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
		err = run(ctx, logger)
	}
	switch {
	case err == nil:
		return command.ExitOK
	case errors.Is(err, errSweepFailed):
	case errors.Is(err, config.ErrInvalid), errors.Is(err, errServerUnsupported), errors.Is(err, errMemoryLock),
		errors.Is(err, errSecretMode),
		errors.Is(err, erase.ErrNotInPlace):
		logger.ErrorContext(ctx, failureMessage, "err", err.Error())
	default:
		logger.ErrorContext(ctx, failureMessage, "err", logging.ErrorText(err))
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

// requireSecretMode refuses a build whose secret mode erases nothing, unless that is explicitly allowed.
func requireSecretMode(development config.Development, logger *slog.Logger) error {
	switch {
	case secretmode.Supported():
		return nil
	case development.AllowUnerasedMemory:
		logger.Warn("memory-not-erased", "reason", "NYLZO_ALLOW_UNERASED_MEMORY")
		return nil
	default:
		return errSecretMode
	}
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
	ctx context.Context,
	dataDir string,
	development config.Development,
	pool disk.Pool,
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
		logger.WarnContext(ctx, "permissions-narrowed", "entries", narrowed)
	}
	if err != nil {
		return nil, nil, err
	}
	blobs, err := disk.New(dataDir, disk.WithPool(pool))
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
	database, err := open(ctx, databasePath)
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
	return requireErasable(epoch.ErasablePaths(directory), settings.Development, logger)
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

// blobPool sizes the pool of upload files the same way in serve, sweep-once and takedown.
func blobPool(settings *config.Server) disk.Pool {
	return disk.PoolFor(settings.Rates.GlobalDailyCreates, settings.SweepInterval, settings.Limits.MaxTTL)
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
	if prepareErr := prepareProcess(&settings, logger); prepareErr != nil {
		return prepareErr
	}
	database, blobs, err := openStore(
		ctx,
		settings.DataDir,
		settings.Development,
		blobPool(&settings),
		upgradingOpen(settings.DataDir, logger),
		logger,
	)
	if err != nil {
		return err
	}
	defer database.Close()
	if database.LogResetSkipped() {
		logger.WarnContext(
			ctx,
			"wal-reset-skipped",
			"notice", "another process had the database open, so the log headers still count earlier writes",
		)
	}
	instance, err := server.New(&settings, database, blobs, server.WithLogger(logger))
	clear(settings.Pepper)
	clear(settings.PepperPrevious)
	if err != nil {
		return err
	}
	database.OnScrubFailure(func(err error) {
		instance.Events().ErrorContext(ctx, "wal-scrub", "err", logging.ErrorText(err))
	})
	if returnErr := instance.ReturnUnstoredCreates(ctx); returnErr != nil {
		return returnErr
	}
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", settings.Listen)
	if err != nil {
		return err
	}
	logPlainListener(ctx, logger, listener.Addr(), len(settings.TrustedProxies))
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var sweeper sync.WaitGroup
	sweeper.Go(func() { instance.RunSweeper(ctx) })
	sweeper.Go(func() { instance.RunHousekeeping(ctx) })
	sweeper.Go(func() { instance.RunRatchet(ctx) })
	httpServer := instance.HTTPServer()
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(server.SecureEarlyErrors(listener)) }()
	logListening(ctx, logger, listener.Addr(), &settings)
	select {
	case err = <-served:
		stop()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	case <-ctx.Done():
		stop()
		logger.InfoContext(ctx, "shutdown", "drain", shutdownTimeout.String())
		err = shutdown(ctx, httpServer, shutdownTimeout, logger)
	}
	waitFor(ctx, &sweeper, shutdownTimeout, logger)
	instance.Close()
	return err
}

func logListening(ctx context.Context, logger *slog.Logger, address net.Addr, settings *config.Server) {
	logger.InfoContext(
		ctx,
		"listening",
		"addr", address.String(),
		"version", settings.Version,
		"commit", settings.Build.Commit,
		"build_date", settings.Build.Date,
		"dirty", settings.Build.Dirty,
	)
}

// prepareProcess refuses a process that would leave keys in swap or in memory and prepares the epoch directory, all
// before any store is opened.
func prepareProcess(settings *config.Server, logger *slog.Logger) error {
	if err := requireMemoryLock(settings.Development, logger); err != nil {
		return err
	}
	if err := requireSecretMode(settings.Development, logger); err != nil {
		return err
	}
	return prepareEpoch(settings, logger)
}

// Behind a declared reverse proxy a plain HTTP listener is the intended setup, not a misconfiguration.
func logPlainListener(ctx context.Context, logger *slog.Logger, address net.Addr, trustedProxies int) {
	if tcp, ok := address.(*net.TCPAddr); ok && tcp.IP.IsLoopback() {
		return
	}
	level := slog.LevelWarn
	if trustedProxies > 0 {
		level = slog.LevelInfo
	}
	logger.Log(ctx, level, "plain-http-listener", "addr", address.String(), "trusted_proxies", trustedProxies)
}

func waitFor(ctx context.Context, group *sync.WaitGroup, timeout time.Duration, logger *slog.Logger) {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		logger.WarnContext(ctx, "sweep-still-running", "after", timeout.String())
	}
}

// shutdown drains open requests after the serving context ended, so it keeps the values but not the cancellation.
func shutdown(ctx context.Context, httpServer *http.Server, timeout time.Duration, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	err := httpServer.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		logger.WarnContext(ctx, "shutdown-timeout", "after", timeout.String())
		return httpServer.Close()
	}
	return err
}

func sweepOnce(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {
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
	database, blobs, err := openStore(
		ctx,
		settings.DataDir,
		settings.Development,
		blobPool(&settings),
		sqlite.OpenCurrent,
		logger,
	)
	if err != nil {
		return err
	}
	defer database.Close()
	now := time.Now()
	_, err = sweep.Run(ctx, database, blobs, settings.DataDir, now)
	err = errors.Join(err, advanceRatchet(&settings, now, logger))
	if err != nil {
		logger.ErrorContext(ctx, "sweep-once", "err", sweep.FirstError(err))
		return errSweepFailed
	}
	return nil
}

// advanceRatchet erases the epoch keys of the hours that have passed while serve does not run; it starts no ratchet,
// and only a directory the operator named must exist.
func advanceRatchet(settings *config.Server, now time.Time, logger *slog.Logger) error {
	options := epoch.Options{CheckFileSystem: func(directory string) error {
		return requireErasable(epoch.ErasablePaths(directory), settings.Development, logger)
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
		ctx,
		settings.DataDir,
		settings.Development,
		blobPool(&settings),
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

var (
	errTakedownInputTooLarge = errors.New("give at most 1 MiB of ids on stdin")
	errLineNotID             = errors.New("is not a message id; give one id per line, never a link")
	errLineTooLong           = errors.New("a line on stdin is too long to be a message id")
	errNoIDs                 = errors.New("give the ids to end on stdin, one per line")
	errTooManyIDs            = fmt.Errorf("give at most %d ids at once", maxTakedownIDs)
)

// readIDs refuses the whole input when one line is not an id, so a pasted link never ends anything.
func readIDs(reader io.Reader) ([]core.ID, error) {
	data, err := command.ReadLimited(reader, maxTakedownInput)
	if err != nil {
		return nil, errTakedownInputTooLarge
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
			return nil, fmt.Errorf("line %d %w", line, errLineNotID)
		}
		if !seen[id.String()] {
			seen[id.String()] = true
			ids = append(ids, id)
		}
	}
	if scanner.Err() != nil {
		return nil, errLineTooLong
	}
	if len(ids) == 0 {
		return nil, errNoIDs
	}
	if len(ids) > maxTakedownIDs {
		return nil, errTooManyIDs
	}
	return ids, nil
}
