package operator

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/harden"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/datadir"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/sweep"
	"github.com/misaon/nylzo/internal/server/transport"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	databaseFile       = "nylzo.db"
	reportDirectory    = "abuse"
	legacyReportFile   = "abuse-reports.jsonl"
	shutdownTimeout    = 30 * time.Second
	defaultMemoryLimit = 192 << 20
	failureMessage     = command.Program
)

var (
	errMemoryLock = errors.New(
		"the server must lock its memory so no key reaches swap: raise the memlock limit " +
			"(systemd LimitMEMLOCK=infinity, docker run --ulimit memlock=-1:-1, Compose ulimits memlock -1) " +
			"or grant CAP_IPC_LOCK outside a user namespace; NYLZO_ALLOW_SWAP=1 serves without the lock",
	)
	errSecretMode = errors.New(
		"this build leaves request keys in memory after use: build with GOEXPERIMENT=runtimesecret for " +
			"linux/amd64 or linux/arm64; NYLZO_ALLOW_UNERASED_MEMORY=1 serves without erasing them",
	)
	errSweepFailed     = errors.New("sweep failed; see the sweep-once log line")
	errNoStore         = errors.New("found no store in NYLZO_DATA_DIR")
	errReportDirectory = errors.New(
		"the abuse reports of an earlier release must be files in a directory of the data directory; remove this " +
			"entry by hand",
	)
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
	if code, done := command.ParseFlags(flags, environment, arguments); done {
		return code
	}
	logger := logging.New(environment.Stderr)
	err := requireSupportedSystem()
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
func requireMemoryLock(development config.Development, lock func() error, logger *slog.Logger) error {
	if development.AllowSwap {
		logger.Warn("memory-not-locked", "reason", "NYLZO_ALLOW_SWAP")
		return nil
	}
	if err := lock(); err != nil {
		return fmt.Errorf("%w: %w", errMemoryLock, err)
	}
	return nil
}

// requireSecretMode refuses a build whose secret mode erases nothing, unless that is explicitly allowed.
func requireSecretMode(erases bool, development config.Development, logger *slog.Logger) error {
	switch {
	case erases:
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
	return allowUnerasable(erase.RequireInPlace(paths...), development, logger)
}

func allowUnerasable(err error, development config.Development, logger *slog.Logger) error {
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
func erasablePaths(dataDirectory string) ([]string, error) {
	databasePath := filepath.Join(dataDirectory, databaseFile)
	blobDirectory := filepath.Join(dataDirectory, disk.Directory)
	temporaryDirectory := filepath.Join(blobDirectory, "tmp")
	paths := append([]string{dataDirectory, blobDirectory, temporaryDirectory}, databaseFiles(databasePath)...)
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
	dataDirectory string,
	development config.Development,
	pool disk.Pool,
	open func(context.Context, string) (*sqlite.Store, error),
	logger *slog.Logger,
) (*sqlite.Store, *disk.Blobs, error) {
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil { //nolint:forbidigo // the server data directory
		return nil, nil, err
	}
	if err := requireErasable([]string{dataDirectory}, development, logger); err != nil {
		return nil, nil, err
	}
	databasePath := filepath.Join(dataDirectory, databaseFile)
	narrowed, err := datadir.Secure(datadir.Layout{
		Directory: dataDirectory,
		Files: slices.Concat(
			[]string{databasePath, databasePath + "-wal", databasePath + "-shm"},
			adminstore.Files(adminStorePath(dataDirectory)),
		),
		Trees: []string{filepath.Join(dataDirectory, disk.Directory)},
	})
	if narrowed > 0 {
		logger.WarnContext(ctx, "permissions-narrowed", "entries", narrowed)
	}
	if err != nil {
		return nil, nil, err
	}
	blobs, err := disk.New(dataDirectory, disk.WithPool(pool))
	var paths []string
	if err == nil {
		paths, err = erasablePaths(dataDirectory)
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
	if _, err := inspectEpoch(directory); err != nil {
		return err
	}
	return requireErasable(epoch.ErasablePaths(directory), settings.Development, logger)
}

// inspectEpoch runs in secret mode, because it reads every seed of the file.
func inspectEpoch(directory string) (epoch.State, error) {
	var (
		state epoch.State
		err   error
	)
	secretmode.Do(func() { state, err = epoch.Inspect(directory) })
	return state, err
}

// storedReports are the abuse reports that releases before Nylzo 0.14.0 left in a data directory; found also tells of
// an empty report directory.
type storedReports struct {
	files []string
	found bool
}

// findReports refuses a report directory that is a link or holds anything but files, because erasing through it
// would overwrite files outside the data directory, and reports that the storage would keep after an overwrite.
func findReports(dataDirectory string, development config.Development, logger *slog.Logger) (storedReports, error) {
	var reports storedReports
	legacyFile := filepath.Join(dataDirectory, legacyReportFile)
	directory := filepath.Join(dataDirectory, reportDirectory)
	legacy, legacyErr := os.Lstat(legacyFile)
	info, err := os.Lstat(directory)
	if statErr := cmp.Or(existingError(legacyErr), existingError(err)); statErr != nil {
		return storedReports{}, statErr
	}
	switch {
	case legacyErr != nil:
	case !legacy.Mode().IsRegular():
		return storedReports{}, fmt.Errorf("%w: %s", errReportDirectory, legacyFile)
	default:
		reports = storedReports{files: []string{legacyFile}, found: true}
	}
	switch {
	case err != nil:
		return reports, requireErasable(reports.files, development, logger)
	case !info.IsDir():
		return storedReports{}, fmt.Errorf("%w: %s", errReportDirectory, directory)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return storedReports{}, err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return storedReports{}, fmt.Errorf("%w: %s", errReportDirectory, filepath.Join(directory, entry.Name()))
		}
		reports.files = append(reports.files, filepath.Join(directory, entry.Name()))
	}
	reports.found = true
	return reports, requireErasable(append([]string{directory}, reports.files...), development, logger)
}

func existingError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// eraseReports overwrites every stored abuse report in full before it unlinks it, because this build neither reads
// nor prunes them; an interrupted erase finishes on the next start.
func eraseReports(
	ctx context.Context,
	dataDirectory string,
	development config.Development,
	logger *slog.Logger,
) error {
	reports, err := findReports(dataDirectory, development, logger)
	if err != nil || !reports.found {
		return err
	}
	for _, path := range reports.files {
		if shredErr := erase.Shred(path, erase.WholeFile); shredErr != nil {
			return shredErr
		}
	}
	err = os.Remove(filepath.Join(dataDirectory, reportDirectory))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if syncErr := erase.SyncDirectory(dataDirectory); syncErr != nil {
		return syncErr
	}
	logger.WarnContext(
		ctx,
		"reports-erased",
		"notice", "nylzo no longer takes abuse reports: the reports that an earlier release stored were erased",
	)
	return nil
}

// upgradingOpen erases a data directory of an earlier protocol version before it opens it, because protocol version 3
// reads none of its messages; an interrupted upgrade finishes on the next start.
func upgradingOpen(
	dataDirectory string,
	development config.Development,
	logger *slog.Logger,
) func(context.Context, string) (*sqlite.Store, error) {
	return func(ctx context.Context, path string) (*sqlite.Store, error) {
		if err := eraseReports(ctx, dataDirectory, development, logger); err != nil {
			return nil, err
		}
		upgraded, err := sqlite.UpgradeFromReleased(ctx, path, func() error { return disk.EraseAll(dataDirectory) })
		if err != nil {
			return nil, err
		}
		if upgraded {
			logger.WarnContext(
				ctx,
				"store-upgraded",
				"notice", "protocol version 3: every message stored before this upgrade was erased",
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
func requireStore(dataDirectory string) error {
	for _, name := range []string{databaseFile, disk.Directory} {
		if _, statErr := os.Stat(filepath.Join(dataDirectory, name)); statErr != nil {
			return fmt.Errorf("%w: %s", errNoStore, name)
		}
	}
	return nil
}

func serveSettings(environment *command.Environment) (config.Server, error) {
	settings, err := config.FromEnvironment(environment.Getenv, true)
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
	settings, err := serveSettings(environment)
	if err != nil {
		return err
	}
	release, err := datadir.Hold(settings.DataDirectory)
	if err != nil {
		return err
	}
	defer release()
	if prepareErr := prepareProcess(&settings, harden.LockMemory, logger); prepareErr != nil {
		return prepareErr
	}
	database, blobs, err := openStore(
		ctx,
		settings.DataDirectory,
		settings.Development,
		blobPool(&settings),
		upgradingOpen(settings.DataDirectory, settings.Development, logger),
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
	opened := withAdminStore(ctx, settings.DataDirectory, database, blobs, logger)
	defer opened.close()
	instance, err := startInstance(ctx, &settings, opened, logger)
	if err != nil {
		return err
	}
	database.OnScrubFailure(scrubFailures(ctx, instance.Events()))
	blobs.OnPoolFailure(poolFailures(ctx, instance.Events()))
	if !database.LogResetSkipped() {
		if returnErr := instance.ReturnUnstoredCreates(ctx); returnErr != nil {
			instance.Close()
			return returnErr
		}
	}
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", settings.Listen)
	if err != nil {
		instance.Close()
		return err
	}
	logPlainListener(ctx, logger, listener.Addr(), len(settings.TrustedProxies))
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	sweeper := startWorkers(ctx, instance)
	logListening(ctx, logger, listener.Addr(), &settings)
	err = serveUntilStopped(ctx, stop, instance.HTTPServer(), listener, logger)
	waitFor(ctx, sweeper, shutdownTimeout, logger)
	instance.Close()
	return err
}

func serveUntilStopped(
	ctx context.Context,
	stop context.CancelFunc,
	httpServer *http.Server,
	listener net.Listener,
	logger *slog.Logger,
) error {
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(transport.SecureEarlyErrors(listener)) }()
	select {
	case err := <-served:
		stop()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop()
		logger.InfoContext(ctx, "shutdown", "drain", shutdownTimeout.String())
		return shutdown(ctx, httpServer, shutdownTimeout, logger)
	}
}

func scrubFailures(ctx context.Context, events *slog.Logger) func(error) {
	return func(err error) {
		events.ErrorContext(ctx, "wal-scrub", "err", logging.ErrorText(err))
	}
}

func poolFailures(ctx context.Context, events *slog.Logger) func(error) {
	return func(err error) {
		events.ErrorContext(ctx, "upload-pool", "err", logging.ErrorText(err))
	}
}

func startWorkers(ctx context.Context, instance *server.Server) *sync.WaitGroup {
	var workers sync.WaitGroup
	for _, run := range []func(context.Context){
		instance.RunSweeper, instance.RunHousekeeping, instance.RunRatchet, instance.RunUsage,
	} {
		workers.Go(func() { run(ctx) })
	}
	return &workers
}

type stores struct {
	database *sqlite.Store
	blobs    *disk.Blobs
	admin    *adminstore.Store
}

// withAdminStore opens the store of the admin area. The server takes messages without it: a file it cannot open
// only switches the admin area and its counting off.
func withAdminStore(
	ctx context.Context,
	dataDirectory string,
	database *sqlite.Store,
	blobs *disk.Blobs,
	logger *slog.Logger,
) *stores {
	admin, err := adminstore.Open(ctx, adminStorePath(dataDirectory))
	if err != nil {
		logger.ErrorContext(ctx, "admin-store-unavailable", "err", logging.ErrorText(err))
		admin = nil
	}
	return &stores{database: database, blobs: blobs, admin: admin}
}

func (s *stores) close() {
	if s.admin != nil {
		s.admin.Close()
	}
}

// startInstance seals the stored rows with the pepper before the server takes a request (spec §5.2).
func startInstance(
	ctx context.Context,
	settings *config.Server,
	opened *stores,
	logger *slog.Logger,
) (*server.Server, error) {
	instance, err := server.New(
		settings,
		opened.database,
		opened.blobs,
		server.WithLogger(logger),
		server.WithAdminStore(opened.admin),
	)
	clear(settings.Pepper)
	clear(settings.PepperPrevious)
	if err != nil {
		return nil, err
	}
	if err = instance.SealRows(ctx); err != nil {
		instance.Close()
		return nil, err
	}
	if err = instance.StartCounting(ctx); err != nil {
		logger.ErrorContext(ctx, "admin-counts", "err", logging.ErrorText(err))
	}
	return instance, nil
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
func prepareProcess(settings *config.Server, lock func() error, logger *slog.Logger) error {
	if err := requireMemoryLock(settings.Development, lock, logger); err != nil {
		return err
	}
	if err := requireSecretMode(secretmode.Supported(), settings.Development, logger); err != nil {
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
	settings, err := config.FromEnvironment(getenv, false)
	if err != nil {
		return err
	}
	if storeErr := requireStore(settings.DataDirectory); storeErr != nil {
		return storeErr
	}
	database, blobs, err := openStore(
		ctx,
		settings.DataDirectory,
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
	_, err = sweep.Run(ctx, database, blobs, now)
	err = errors.Join(err, advanceRatchet(&settings, time.Now(), logger))
	if err != nil {
		logger.ErrorContext(ctx, "sweep-once", "err", sweep.FirstError(err))
		return errSweepFailed
	}
	return nil
}

// advanceRatchet erases the epoch keys of the minutes that have passed while serve does not run; it starts no ratchet,
// and only a directory the operator named must exist.
func advanceRatchet(settings *config.Server, now time.Time, logger *slog.Logger) error {
	options := epoch.Options{
		CheckFileSystem: func(directory string) error {
			return requireErasable(epoch.ErasablePaths(directory), settings.Development, logger)
		},
		Replaced: func() { server.LogReplacedGeneration(logger) },
	}
	var ratchet *epoch.Ratchet
	var err error
	secretmode.Do(func() { ratchet, _, err = epoch.Open(settings.EpochDirectory(), now, options) })
	if ratchet != nil {
		ratchet.Close()
	}
	if errors.Is(err, epoch.ErrNoDirectory) && settings.EpochDirectoryOverride == "" {
		return nil
	}
	return err
}
