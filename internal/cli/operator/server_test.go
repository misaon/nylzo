package operator

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/datadir"
	"github.com/misaon/nylzo/internal/store"

	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestSweepOnceNeedsAnExistingStore(t *testing.T) {
	directory := t.TempDir()
	if err := requireStore(directory); err == nil {
		t.Fatal("sweep-once accepted an empty data directory")
	}
	if err := os.WriteFile(filepath.Join(directory, databaseFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireStore(directory); err == nil {
		t.Fatal("sweep-once accepted a store without a blob directory")
	}
	if err := os.Mkdir(filepath.Join(directory, disk.Directory), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := requireStore(directory); err != nil {
		t.Fatalf("sweep-once refused an existing store: %v", err)
	}
}

func TestShutdownPastItsTimeoutIsAnOrderlyExit(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{})
	httpServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	})}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	go http.Get("http://" + listener.Addr().String())
	<-entered
	if err = shutdown(t.Context(), httpServer, 50*time.Millisecond, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("a shutdown that outlives its timeout must still end cleanly: %v", err)
	}
	if err = <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("server still serving: %v", err)
	}
}

func TestServeSetsASoftMemoryLimitUnlessGOMEMLIMITIsSet(t *testing.T) {
	previous := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(previous) })
	for _, testCase := range []struct {
		operatorLimit string
		want          int64
	}{{"", defaultMemoryLimit}, {"300MiB", math.MaxInt64}} {
		debug.SetMemoryLimit(math.MaxInt64)
		variables := map[string]string{
			"NYLZO_PEPPER_FILE": pepperFile(t),
			"NYLZO_DATA_DIR":    t.TempDir(),
			"GOMEMLIMIT":        testCase.operatorLimit,
		}
		if _, err := serveSettings(
			&command.Environment{Getenv: func(name string) string { return variables[name] }},
		); err != nil {
			t.Fatal(err)
		}
		if got := debug.SetMemoryLimit(-1); got != testCase.want {
			t.Fatalf("GOMEMLIMIT %q: the soft memory limit is %d, want %d", testCase.operatorLimit, got, testCase.want)
		}
	}
}

func TestRequireStoreAcceptsWhatOpenCreates(t *testing.T) {
	directory := t.TempDir()
	storage, _, err := openStore(
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
	storage.Close()
	if err = requireStore(directory); err != nil {
		t.Fatalf("sweep-once must accept a store that serve created: %v", err)
	}
}

func TestSweepOnceLeavesAMissingStoreAlone(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent")
	if err := sweepOnce(
		t.Context(),
		getenvFrom(map[string]string{"NYLZO_DATA_DIR": directory}),
		slog.New(slog.DiscardHandler),
	); !errors.Is(
		err,
		errNoStore,
	) {
		t.Fatalf("sweep-once ran against a missing store: %v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sweep-once created the data directory: %v", err)
	}
}

func TestConfigErrorsNameTheVariable(t *testing.T) {
	result := run(t, "", map[string]string{"NYLZO_RATE_SOURCE_BURST": "0.5"}, nil, RunSweepOnce)
	if result.code != 1 || !strings.Contains(result.stderr, "NYLZO_RATE_SOURCE_BURST must be at least 1") {
		t.Fatalf("exit %d, stderr %q", result.code, result.stderr)
	}
}

func TestSweepOnceNeedsNoPepper(t *testing.T) {
	directory := t.TempDir()
	storage, _, err := openStore(
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
	storage.Close()
	result := run(t, "", map[string]string{"NYLZO_DATA_DIR": directory}, nil, RunSweepOnce)
	if result.code != 0 {
		t.Fatalf("sweep-once without a pepper: exit %d, %s", result.code, result.stderr)
	}
	if strings.Count(result.stderr, `"level":"ERROR"`) != 0 || strings.Contains(result.stderr, `"msg":"sweep-once"`) {
		t.Fatalf("a clean sweep logged an error or a report: %s", result.stderr)
	}
}

func TestServeChecksThePortBeforeItSweeps(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	result := run(
		t,
		"",
		map[string]string{
			"NYLZO_DATA_DIR":    t.TempDir(),
			"NYLZO_PEPPER_FILE": pepperFile(t),
			"NYLZO_LISTEN":      taken.Addr().String(),
		},
		nil,
		RunServe,
	)
	if result.code != 1 || strings.Contains(result.stderr, `"msg":"sweep`) ||
		strings.Contains(result.stderr, `"msg":"listening"`) {
		t.Fatalf("a taken port: exit %d, %s", result.code, result.stderr)
	}
}

func TestServeRefusesADataDirectoryThatAnotherServeHolds(t *testing.T) {
	directory := t.TempDir()
	release, err := datadir.Hold(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	result := run(
		t,
		"",
		map[string]string{
			"NYLZO_DATA_DIR":    directory,
			"NYLZO_PEPPER_FILE": pepperFile(t),
			"NYLZO_LISTEN":      taken.Addr().String(),
		},
		nil,
		RunServe,
	)
	if result.code != 1 || !strings.Contains(result.stderr, datadir.ErrServed.Error()) {
		t.Fatalf("a held data directory: exit %d, %s", result.code, result.stderr)
	}
}

func TestServeRefusesABuildThatLeavesKeysInMemory(t *testing.T) {
	if secretmode.Supported() {
		t.Skip("this test binary erases request memory")
	}
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	result := run(
		t,
		"",
		map[string]string{
			"NYLZO_DATA_DIR":              t.TempDir(),
			"NYLZO_PEPPER_FILE":           pepperFile(t),
			"NYLZO_LISTEN":                taken.Addr().String(),
			"NYLZO_ALLOW_UNERASED_MEMORY": "0",
		},
		nil,
		RunServe,
	)
	if result.code != 1 || !strings.Contains(result.stderr, "GOEXPERIMENT=runtimesecret") {
		t.Fatalf("a build without secret mode: exit %d, %s", result.code, result.stderr)
	}
}

func TestLogLinesOfTheCommandCarryTheMinuteOnly(t *testing.T) {
	stderr := run(
		t,
		"",
		map[string]string{"NYLZO_DATA_DIR": filepath.Join(t.TempDir(), "absent")},
		nil,
		RunSweepOnce,
	).stderr
	var line struct {
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal(
		[]byte(stderr),
		&line,
	); err != nil || line.Time.IsZero() ||
		!line.Time.Equal(line.Time.Truncate(time.Minute)) {
		t.Fatalf("a log line of nylzo carries a time finer than the minute: %q %v", stderr, err)
	}
}

func TestOpenCreatesPrivateDirectories(t *testing.T) {
	settings := config.Defaults()
	settings.DataDirectory = filepath.Join(t.TempDir(), "data")
	storage, _, err := openStore(
		t.Context(),
		settings.DataDirectory,
		testDevelopment,
		testPool,
		sqlite.Open,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	storage.Close()
	for _, directory := range []string{settings.DataDirectory, filepath.Join(settings.DataDirectory, disk.Directory), filepath.Join(settings.DataDirectory, disk.Directory, "tmp")} {
		info, statErr := os.Stat(directory)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is open to other users: %v", directory, info.Mode().Perm())
		}
	}
}

func TestSweepOnceRefusesAnotherSchemaVersion(t *testing.T) {
	directory := t.TempDir()
	storage, _, err := openStore(
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
	storage.Close()
	database, err := sql.Open("sqlite", "file:"+filepath.Join(directory, databaseFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`PRAGMA user_version = 3`); err != nil {
		t.Fatal(err)
	}
	database.Close()
	result := run(t, "", map[string]string{"NYLZO_DATA_DIR": directory}, nil, RunSweepOnce)
	if result.code != 1 || !strings.Contains(result.stderr, "development build") {
		t.Fatalf("sweep-once on an older schema: exit %d, %s", result.code, result.stderr)
	}
	database, err = sql.Open("sqlite", "file:"+filepath.Join(directory, databaseFile))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var version int
	if err = database.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 3 {
		t.Fatalf("sweep-once changed the version of a database it refused: version %d %v", version, err)
	}
	if _, err = database.Exec(`PRAGMA user_version = 9`); err != nil {
		t.Fatal(err)
	}
	if result = run(t, "", map[string]string{"NYLZO_DATA_DIR": directory}, nil, RunSweepOnce); result.code != 1 ||
		!strings.Contains(result.stderr, "newer nylzo") {
		t.Fatalf("sweep-once on a newer schema: exit %d, %s", result.code, result.stderr)
	}
}

func TestServeSealsTheWrapsOfThePreviousPepperBeforeItListens(t *testing.T) {
	directory := t.TempDir()
	id := storeMessage(t, directory)
	locator := seal.LocatorOf(id[:])
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
	previousFile := filepath.Join(t.TempDir(), "previous")
	if err = os.WriteFile(previousFile, []byte(strings.Repeat("fedcba9876543210", 4)), 0o400); err != nil {
		t.Fatal(err)
	}
	variables := map[string]string{
		"NYLZO_PEPPER_FILE":          pepperFile(t),
		"NYLZO_PEPPER_PREVIOUS_FILE": previousFile,
		"NYLZO_DATA_DIR":             directory,
		"GOMEMLIMIT":                 "1GiB",
	}
	settings, err := serveSettings(&command.Environment{Getenv: getenvFrom(variables)})
	if err != nil {
		t.Fatal(err)
	}
	settings.Development = testDevelopment
	previous, err := pepper.NewRing(settings.PepperPrevious, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := pepper.NewRing(settings.Pepper, nil)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, recordKey := core.NewSecret(), seal.NewKey()
	err = database.Update(t.Context(), locator, func(row *store.Row) error {
		row.ReadWrap = seal.WrapRecordKey(previous.RecordsKey(), publicKey[:], recordKey, locator, seal.Read)
		row.ManageWrap = seal.WrapRecordKey(previous.RecordsKey(), publicKey[:], recordKey, locator, seal.Manage)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := startInstance(t.Context(), &settings, database, blobs, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	instance.Close()
	row, err := database.Load(t.Context(), locator)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := seal.UnwrapRecordKey(current.RecordsKey(), publicKey[:], row.ReadWrap, locator, seal.Read)
	if err != nil || opened != recordKey {
		t.Fatalf("serve left the read wrap under the previous pepper: %v", err)
	}
}

// Erasing through a linked report directory would overwrite files outside the data directory, so serve refuses it and
// store-check says so before an update switches to this build.
func TestServeRefusesAReportDirectoryThatLeadsElsewhere(t *testing.T) {
	directory, outside := t.TempDir(), t.TempDir()
	kept := filepath.Join(outside, "kept")
	if err := os.WriteFile(kept, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, reportDirectory)); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)
	if err := eraseReports(t.Context(), directory, testDevelopment, logger); !errors.Is(err, errReportDirectory) {
		t.Fatalf("erasing through a linked report directory: %v", err)
	}
	if check := run(t, "", map[string]string{"NYLZO_DATA_DIR": directory}, nil, RunStoreCheck); check.code !=
		command.ExitError {
		t.Fatalf("store-check passed a linked report directory: %s", check.stdout)
	}
	if content, err := os.ReadFile(kept); err != nil || string(content) != "keep" {
		t.Fatalf("the erase wrote outside the data directory: %q %v", content, err)
	}
}

// A second link to each file shows its bytes after the start: only an overwrite in place turns them into zeros.
func TestServeErasesTheAbuseReportsOfEarlierReleases(t *testing.T) {
	directory, keep := t.TempDir(), t.TempDir()
	content := bytes.Repeat([]byte{0xa5}, 2944)
	names := []string{
		legacyReportFile,
		filepath.Join(reportDirectory, "2026-11-14.sealed"),
		filepath.Join(reportDirectory, "2026-10-20.jsonl"),
	}
	if err := os.Mkdir(filepath.Join(directory, reportDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, filepath.Join(keep, filepath.Base(name))); err != nil {
			t.Fatal(err)
		}
	}
	check := run(t, "", map[string]string{"NYLZO_DATA_DIR": directory}, nil, RunStoreCheck)
	if check.code != command.ExitOK || !strings.Contains(check.stdout, "reports: abuse reports of an earlier release") {
		t.Fatalf("store-check did not announce the erase: exit %d, %s%s", check.code, check.stdout, check.stderr)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	for range 2 {
		storage, _, err := openStore(
			t.Context(),
			directory,
			testDevelopment,
			testPool,
			upgradingOpen(directory, testDevelopment, logger),
			logger,
		)
		if err != nil {
			t.Fatal(err)
		}
		storage.Close()
	}
	if strings.Count(logs.String(), `"msg":"reports-erased"`) != 1 {
		t.Fatalf("the start that erased reports must warn once, and a later start not at all: %s", logs.String())
	}
	if _, err := os.Lstat(filepath.Join(directory, reportDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the report directory is still in the data directory: %v", err)
	}
	for _, name := range names {
		left, err := os.ReadFile(filepath.Join(keep, filepath.Base(name)))
		if err != nil || !bytes.Equal(left, make([]byte, len(content))) {
			t.Fatalf("%s kept some of its bytes: %v", name, err)
		}
		if _, err = os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s is still in the data directory: %v", name, err)
		}
	}
}
