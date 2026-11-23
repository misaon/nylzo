package operator

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

type watchedLog struct {
	mutex     sync.Mutex
	lines     bytes.Buffer
	listening chan struct{}
	once      sync.Once
	stopEvent string
	stop      func()
}

func newWatchedLog() *watchedLog {
	return &watchedLog{listening: make(chan struct{})}
}

func (l *watchedLog) Write(line []byte) (int, error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if bytes.Contains(line, []byte(`"msg":"listening"`)) {
		l.once.Do(func() { close(l.listening) })
	}
	if l.stop != nil && bytes.Contains(line, []byte(`"msg":"`+l.stopEvent+`"`)) {
		l.stop()
	}
	return l.lines.Write(line)
}

func (l *watchedLog) String() string {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.lines.String()
}

func recordingLogger() (*slog.Logger, *watchedLog) {
	lines := newWatchedLog()
	return slog.New(slog.NewJSONHandler(lines, nil)), lines
}

// Outside Linux nylzo knows no file system that overwrites in place, so any directory is unerasable there.
func unerasableDirectory(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "linux" {
		return "/proc/self/"
	}
	return t.TempDir()
}

func serveVariables(t *testing.T, directory string) map[string]string {
	t.Helper()
	return map[string]string{
		"NYLZO_DATA_DIR":    directory,
		"NYLZO_PEPPER_FILE": pepperFile(t),
		"NYLZO_LISTEN":      "127.0.0.1:0",
	}
}

func serveEnvironment(variables map[string]string, stderr *watchedLog) *command.Environment {
	for name, allowed := range map[string]bool{
		"NYLZO_ALLOW_SWAP":               testDevelopment.AllowSwap,
		"NYLZO_ALLOW_UNERASABLE_STORAGE": testDevelopment.AllowUnerasableStorage,
		"NYLZO_ALLOW_UNERASED_MEMORY":    testDevelopment.AllowUnerasedMemory,
	} {
		if allowed {
			variables[name] = "1"
		}
	}
	return &command.Environment{
		Stdin:    strings.NewReader(""),
		Stdout:   new(bytes.Buffer),
		Stderr:   stderr,
		Getenv:   getenvFrom(variables),
		Terminal: noTerminal{},
		Version:  "test",
	}
}

func createStore(t *testing.T, directory string) {
	t.Helper()
	database, _, err := openStore(
		t.Context(),
		directory,
		testDevelopment,
		testPool,
		sqlite.Open,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
}

func TestServeKeepsTheLogOfAHeldDatabaseAndDrainsWhenItsContextEnds(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	createStore(t, directory)
	holder, err := sql.Open("sqlite", "file:"+filepath.Join(directory, databaseFile)+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if _, err = holder.Exec(`INSERT INTO rate_counters VALUES ('held', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	stderr := serveUntilListening(t, serveVariables(t, directory))
	for _, event := range []string{"wal-reset-skipped", "shutdown"} {
		if !strings.Contains(stderr, `"msg":"`+event+`"`) {
			t.Errorf("serve did not log %s: %s", event, stderr)
		}
	}
}

func serveUntilListening(t *testing.T, variables map[string]string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stderr := newWatchedLog()
	exited := make(chan int, 1)
	go func() { exited <- RunServe(ctx, serveEnvironment(variables, stderr), nil) }()
	select {
	case <-stderr.listening:
	case code := <-exited:
		t.Fatalf("serve ended with %d before it listened: %s", code, stderr)
	}
	cancel()
	if code := <-exited; code != command.ExitOK {
		t.Fatalf("serve ended with %d after its context ended: %s", code, stderr)
	}
	return stderr.String()
}

func TestServeStopsWhenTheStoreCannotOpen(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, disk.Directory), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stderr := newWatchedLog()
	if code := RunServe(t.Context(), serveEnvironment(serveVariables(t, directory), stderr), nil); code !=
		command.ExitError {
		t.Fatalf("serve with a file for its blob directory: exit %d, %s", code, stderr)
	}
}

func TestServeStopsWhenItsSettingsAreInvalid(t *testing.T) {
	t.Parallel()
	stderr := newWatchedLog()
	variables := map[string]string{"NYLZO_DATA_DIR": t.TempDir()}
	if code := RunServe(t.Context(), serveEnvironment(variables, stderr), nil); code != command.ExitError ||
		!strings.Contains(stderr.String(), "NYLZO_PEPPER_FILE is required") {
		t.Fatalf("serve without a pepper: exit %d, %s", code, stderr)
	}
}

func storeRowOfThePreviousPepper(t *testing.T, directory string) map[string]string {
	t.Helper()
	id := storeMessage(t, directory)
	locator := seal.LocatorOf(id[:])
	previousFile := filepath.Join(t.TempDir(), "previous")
	if err := os.WriteFile(previousFile, []byte(strings.Repeat("fedcba9876543210", 4)), 0o400); err != nil {
		t.Fatal(err)
	}
	variables := serveVariables(t, directory)
	variables["NYLZO_PEPPER_PREVIOUS_FILE"] = previousFile
	settings, err := serveSettings(&command.Environment{Getenv: getenvFrom(variables)})
	if err != nil {
		t.Fatal(err)
	}
	previous, err := pepper.NewRing(settings.PepperPrevious, nil)
	if err != nil {
		t.Fatal(err)
	}
	database, _, err := openStore(
		t.Context(),
		directory,
		testDevelopment,
		testPool,
		sqlite.Open,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, recordKey := core.NewSecret(), seal.NewKey()
	err = database.Update(t.Context(), locator, func(row *store.Row) error {
		row.ReadWrap = seal.WrapRecordKey(previous.RecordsKey(), publicKey[:], recordKey, locator, seal.Read)
		return nil
	})
	database.Close()
	if err != nil {
		t.Fatal(err)
	}
	return variables
}

func TestServeStopsBeforeItListensWhenTheRowsCannotBeSealed(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	variables := storeRowOfThePreviousPepper(t, directory)
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(directory, databaseFile))
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(
		`CREATE TRIGGER refuse BEFORE UPDATE ON messages BEGIN SELECT RAISE(ABORT, 'update refused'); END`,
	)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}
	stderr := newWatchedLog()
	code := RunServe(t.Context(), serveEnvironment(variables, stderr), nil)
	if code != command.ExitError || !strings.Contains(stderr.String(), "update refused") ||
		strings.Contains(stderr.String(), `"msg":"listening"`) {
		t.Fatalf("serve on a store that refuses to seal its rows: exit %d, %s", code, stderr)
	}
}

func TestServeStoppedDuringItsStartNeverListens(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	variables := storeRowOfThePreviousPepper(t, directory)
	variables["NYLZO_GLOBAL_DAILY_CREATES"] = "1000"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stderr := newWatchedLog()
	stderr.stopEvent, stderr.stop = "rows-sealed", cancel
	code := RunServe(ctx, serveEnvironment(variables, stderr), nil)
	if code != command.ExitError || !strings.Contains(stderr.String(), context.Canceled.Error()) ||
		strings.Contains(stderr.String(), `"msg":"listening"`) {
		t.Fatalf("serve stopped during its start: exit %d, %s", code, stderr)
	}
}

func TestServeNeedsTheMemoryLockUnlessSwapIsAllowed(t *testing.T) {
	t.Parallel()
	refused := errors.New("memlock limit")
	settings := config.Server{DataDirectory: t.TempDir()}
	if err := prepareProcess(&settings, func() error { return refused }, slog.New(slog.DiscardHandler)); !errors.Is(
		err,
		errMemoryLock,
	) {
		t.Fatalf("serve without the memory lock: %v", err)
	}
	logger, lines := recordingLogger()
	if err := requireMemoryLock(config.Development{}, func() error { return refused }, logger); !errors.Is(
		err,
		errMemoryLock,
	) || !errors.Is(err, refused) {
		t.Fatalf("a refused lock: %v", err)
	}
	if err := requireMemoryLock(config.Development{}, func() error { return nil }, logger); err != nil {
		t.Fatalf("a granted lock: %v", err)
	}
	allowed := config.Development{AllowSwap: true}
	if err := requireMemoryLock(allowed, func() error { return refused }, logger); err != nil ||
		!strings.Contains(lines.String(), `"msg":"memory-not-locked"`) {
		t.Fatalf("NYLZO_ALLOW_SWAP must serve with a warning: %v %s", err, lines)
	}
}

func TestServeNeedsSecretModeUnlessUnerasedMemoryIsAllowed(t *testing.T) {
	t.Parallel()
	logger, lines := recordingLogger()
	if err := requireSecretMode(true, config.Development{}, logger); err != nil {
		t.Fatalf("a build that erases: %v", err)
	}
	if err := requireSecretMode(false, config.Development{}, logger); !errors.Is(err, errSecretMode) {
		t.Fatalf("a build that erases nothing: %v", err)
	}
	if err := requireSecretMode(false, config.Development{AllowUnerasedMemory: true}, logger); err != nil ||
		!strings.Contains(lines.String(), `"msg":"memory-not-erased"`) {
		t.Fatalf("NYLZO_ALLOW_UNERASED_MEMORY must serve with a warning: %v %s", err, lines)
	}
}

func TestStorageThatMayKeepOldBlocksIsRefusedUnlessAllowed(t *testing.T) {
	t.Parallel()
	unerasable := []string{unerasableDirectory(t)}
	logger, lines := recordingLogger()
	err := requireErasable(unerasable, config.Development{}, logger)
	if !errors.Is(err, erase.ErrNotInPlace) || !strings.Contains(err.Error(), "NYLZO_ALLOW_UNERASABLE_STORAGE") {
		t.Fatalf("storage that may keep old blocks: %v", err)
	}
	if err = requireErasable(unerasable, config.Development{AllowUnerasableStorage: true}, logger); err != nil ||
		!strings.Contains(lines.String(), `"msg":"storage-not-erasable"`) {
		t.Fatalf("NYLZO_ALLOW_UNERASABLE_STORAGE must go on with a warning: %v %s", err, lines)
	}
	unreadable := errors.New("erase: permission denied")
	if err = allowUnerasable(
		unreadable,
		config.Development{AllowUnerasableStorage: true},
		logger,
	); !errors.Is(
		err,
		unreadable,
	) {
		t.Fatalf("a check that failed must stop even when unerasable storage is allowed: %v", err)
	}
}

func TestOpenStoreRefusesADataDirectoryItCannotSecure(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	linkedLog := t.TempDir()
	if err := os.Symlink(file, filepath.Join(linkedLog, databaseFile+"-wal")); err != nil {
		t.Fatal(err)
	}
	blobFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(blobFile, disk.Directory), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	failingOpen := errors.New("open failed")
	for name, testCase := range map[string]struct {
		directory   string
		development config.Development
		open        func(context.Context, string) (*sqlite.Store, error)
	}{
		"under a file":          {filepath.Join(file, "data"), testDevelopment, sqlite.Open},
		"on unerasable storage": {unerasableDirectory(t), config.Development{}, sqlite.Open},
		"with a linked log":     {linkedLog, testDevelopment, sqlite.Open},
		"with a blob file":      {blobFile, testDevelopment, sqlite.Open},
		"that fails to open": {t.TempDir(), testDevelopment, func(context.Context, string) (*sqlite.Store, error) {
			return nil, failingOpen
		}},
	} {
		database, _, err := openStore(
			t.Context(),
			testCase.directory,
			testCase.development,
			testPool,
			testCase.open,
			slog.New(slog.DiscardHandler),
		)
		if err == nil {
			database.Close()
			t.Errorf("a data directory %s was opened", name)
		}
	}
}

// The database files are checked again once the store has opened, so a log that appeared on other storage closes it.
func TestOpenStoreClosesADatabaseWhoseFilesMayKeepOldBlocks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("only Linux knows file systems that overwrite in place")
	}
	directory := t.TempDir()
	var opened *sqlite.Store
	open := func(ctx context.Context, _ string) (*sqlite.Store, error) {
		database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), databaseFile))
		if err != nil {
			return nil, err
		}
		opened = database
		return database, os.Symlink("/proc/self/status", filepath.Join(directory, databaseFile))
	}
	_, _, err := openStore(t.Context(), directory, config.Development{}, testPool, open, slog.New(slog.DiscardHandler))
	if !errors.Is(err, erase.ErrNotInPlace) {
		t.Fatalf("a database file on /proc: %v", err)
	}
	if _, loadErr := opened.Load(t.Context(), seal.Locator{}); loadErr == nil || errors.Is(loadErr, store.ErrNotFound) {
		t.Fatal("openStore left the refused database open")
	}
}

func TestErasablePathsNeedTheBlobDirectories(t *testing.T) {
	t.Parallel()
	if _, err := erasablePaths(t.TempDir()); err == nil {
		t.Fatal("a data directory without blob directories listed its erasable paths")
	}
}

func TestPrepareEpochRefusesADirectoryItCannotUse(t *testing.T) {
	t.Parallel()
	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, "epoch"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	damaged := t.TempDir()
	if err := os.Mkdir(filepath.Join(damaged, "epoch"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(damaged, "epoch", epoch.FileName), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{blocked, damaged} {
		settings := config.Server{DataDirectory: directory, Development: testDevelopment}
		if err := prepareEpoch(&settings, slog.New(slog.DiscardHandler)); err == nil {
			t.Errorf("the epoch directory of %s was accepted", directory)
		}
	}
}

func TestReportsThatCannotBeErasedSafelyAreRefused(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(legacyDirectory, legacyReportFile), 0o700); err != nil {
		t.Fatal(err)
	}
	nested := t.TempDir()
	if err := os.MkdirAll(filepath.Join(nested, reportDirectory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	reportFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(reportFile, reportDirectory), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)
	if _, err := findReports(file, testDevelopment, logger); err == nil || errors.Is(err, errReportDirectory) {
		t.Errorf("a data directory that is a file: %v", err)
	}
	for _, directory := range []string{legacyDirectory, nested, reportFile} {
		if _, err := findReports(directory, testDevelopment, logger); !errors.Is(err, errReportDirectory) {
			t.Errorf("%s: %v", directory, err)
		}
	}
}

func TestAnEraseOfReportsThatCannotFinishFails(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions this test relies on")
	}
	for name, testCase := range map[string]struct {
		reportMode, dataMode os.FileMode
	}{
		"a report it cannot overwrite": {0o400, 0o700},
		"a directory it cannot remove": {0o600, 0o500},
		"a directory it cannot sync":   {0o600, 0o300},
	} {
		directory := t.TempDir()
		if err := os.Mkdir(filepath.Join(directory, reportDirectory), 0o700); err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(directory, reportDirectory, "2026-10-20.jsonl")
		if err := os.WriteFile(report, []byte("report"), testCase.reportMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, testCase.dataMode); err != nil {
			t.Fatal(err)
		}
		err := eraseReports(t.Context(), directory, testDevelopment, slog.New(slog.DiscardHandler))
		if chmodErr := os.Chmod(directory, 0o700); chmodErr != nil {
			t.Fatal(chmodErr)
		}
		if err == nil {
			t.Errorf("%s: the erase reported success", name)
		}
	}
}

func TestAnUnreadableReportDirectoryIsRefused(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions this test relies on")
	}
	directory := t.TempDir()
	reports := filepath.Join(directory, reportDirectory)
	if err := os.Mkdir(reports, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(reports, 0o700) })
	if _, err := findReports(directory, testDevelopment, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("an unreadable report directory was accepted")
	}
}

func TestAnUpgradeThatCannotStartOpensNothing(t *testing.T) {
	t.Parallel()
	linked, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(linked, reportDirectory)); err != nil {
		t.Fatal(err)
	}
	damaged := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(damaged, databaseFile),
		bytes.Repeat([]byte("damaged"), 1000),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)
	for _, directory := range []string{linked, damaged} {
		open := upgradingOpen(directory, testDevelopment, logger)
		if database, err := open(t.Context(), filepath.Join(directory, databaseFile)); err == nil {
			database.Close()
			t.Errorf("%s was opened", directory)
		}
	}
}

func TestServeSettingsRefuseAnInvalidConfiguration(t *testing.T) {
	t.Parallel()
	variables := map[string]string{"NYLZO_DATA_DIR": t.TempDir()}
	if _, err := serveSettings(
		&command.Environment{Getenv: getenvFrom(variables)},
	); !errors.Is(
		err,
		config.ErrInvalid,
	) {
		t.Fatalf("serve settings without a pepper: %v", err)
	}
}

func TestADamagedAdminStoreSwitchesTheAdminAreaOff(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(adminStorePath(directory), bytes.Repeat([]byte("damaged"), 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, lines := recordingLogger()
	opened := withAdminStore(t.Context(), directory, nil, nil, logger)
	defer opened.close()
	if opened.admin != nil || !strings.Contains(lines.String(), `"msg":"admin-store-unavailable"`) {
		t.Fatalf("a damaged admin database: %+v %s", opened, lines)
	}
}

func TestStartInstanceStopsWhenTheServerCannotStart(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database, blobs, err := openStore(
		t.Context(),
		directory,
		testDevelopment,
		testPool,
		sqlite.Open,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Defaults()
	settings.DataDirectory = directory
	settings.Development = testDevelopment
	if _, err = startInstance(
		t.Context(),
		&settings,
		&stores{database: database, blobs: blobs},
		slog.New(slog.DiscardHandler),
	); err == nil {
		t.Fatal("a server without a pepper started")
	}
	settings.Pepper = []byte(strings.Repeat("p", 32))
	database.Close()
	if _, err = startInstance(
		t.Context(),
		&settings,
		&stores{database: database, blobs: blobs},
		slog.New(slog.DiscardHandler),
	); err == nil {
		t.Fatal("a server whose rows cannot be sealed started")
	}
}

func TestAdminCountsThatCannotRestoreOnlyLogAnError(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database, blobs, err := openStore(
		t.Context(),
		directory,
		testDevelopment,
		testPool,
		sqlite.Open,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	admin, err := adminstore.Open(t.Context(), adminStorePath(directory))
	if err != nil {
		t.Fatal(err)
	}
	admin.Close()
	settings := config.Defaults()
	settings.DataDirectory = directory
	settings.Development = testDevelopment
	settings.Pepper = []byte(strings.Repeat("p", 32))
	logger, lines := recordingLogger()
	instance, err := startInstance(
		t.Context(),
		&settings,
		&stores{database: database, blobs: blobs, admin: admin},
		logger,
	)
	if err != nil {
		t.Fatal(err)
	}
	instance.Close()
	if !strings.Contains(lines.String(), `"msg":"admin-counts"`) {
		t.Fatalf("counts that could not restore logged nothing: %s", lines)
	}
}

func TestAPlainListenerOutsideLoopbackIsLogged(t *testing.T) {
	t.Parallel()
	address := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 8080}
	for trustedProxies, level := range map[int]string{0: "WARN", 1: "INFO"} {
		logger, lines := recordingLogger()
		logPlainListener(t.Context(), logger, address, trustedProxies)
		want := fmt.Sprintf(`"level":%q,"msg":"plain-http-listener"`, level)
		if !strings.Contains(lines.String(), want) {
			t.Errorf("%d trusted proxies: %s", trustedProxies, lines)
		}
	}
	logger, lines := recordingLogger()
	logPlainListener(t.Context(), logger, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}, 0)
	if lines.String() != "" {
		t.Errorf("a loopback listener was logged: %s", lines)
	}
}

func TestAWorkerThatOutlivesTheShutdownIsLogged(t *testing.T) {
	t.Parallel()
	var workers sync.WaitGroup
	release := make(chan struct{})
	workers.Go(func() { <-release })
	defer close(release)
	logger, lines := recordingLogger()
	waitFor(t.Context(), &workers, time.Millisecond, logger)
	if !strings.Contains(lines.String(), `"msg":"sweep-still-running"`) {
		t.Fatalf("a worker past the timeout was not logged: %s", lines)
	}
}

// The listener can also end serving: a closed server is an orderly end, a failed listener is not.
func TestServingEndsWithItsListener(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		end     func(*http.Server, net.Listener)
		failure bool
	}{
		"closed server":   {func(httpServer *http.Server, _ net.Listener) { httpServer.Close() }, false},
		"failed listener": {func(_ *http.Server, listener net.Listener) { listener.Close() }, true},
	} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		httpServer := &http.Server{ReadHeaderTimeout: time.Second}
		testCase.end(httpServer, listener)
		ctx, stop := context.WithCancel(t.Context())
		err = serveUntilStopped(ctx, stop, httpServer, listener, slog.New(slog.DiscardHandler))
		if (err != nil) != testCase.failure || ctx.Err() == nil {
			t.Errorf("%s: %v, workers stopped: %v", name, err, ctx.Err() != nil)
		}
		listener.Close()
	}
}

func TestStoreFailuresAreLoggedAsEvents(t *testing.T) {
	t.Parallel()
	for event, report := range map[string]func(context.Context, *slog.Logger) func(error){
		"wal-scrub":   scrubFailures,
		"upload-pool": poolFailures,
	} {
		logger, lines := recordingLogger()
		report(t.Context(), logger)(errors.New("disk full"))
		if !strings.Contains(lines.String(), `"msg":"`+event+`"`) || !strings.Contains(lines.String(), "disk full") {
			t.Errorf("a failure of %s: %s", event, lines)
		}
	}
}

func TestSweepOnceFailsWhenTheNamedEpochDirectoryIsMissing(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	createStore(t, directory)
	result := run(t, "", map[string]string{
		"NYLZO_DATA_DIR":  directory,
		"NYLZO_EPOCH_DIR": filepath.Join(t.TempDir(), "absent"),
	}, nil, RunSweepOnce)
	if result.code != command.ExitError || !strings.Contains(result.stderr, `"msg":"sweep-once"`) {
		t.Fatalf("sweep-once without the named epoch directory: exit %d, %s", result.code, result.stderr)
	}
}

func TestAdvanceRatchetMovesTheSeedOfServeForward(t *testing.T) {
	t.Parallel()
	settings := config.Server{DataDirectory: t.TempDir(), Development: testDevelopment}
	directory := settings.EpochDirectory()
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ratchet, _, err := epoch.Open(directory, now, epoch.Options{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	ratchet.Close()
	logger, lines := recordingLogger()
	if err = advanceRatchet(&settings, now.Add(-2*time.Hour), logger); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lines.String(), `"msg":"epoch-generation-replaced"`) {
		t.Fatalf("a clock behind the seed was not logged: %s", lines)
	}
}
