package operator

import (
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
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"

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
	if err = shutdown(httpServer, 50*time.Millisecond, slog.New(slog.DiscardHandler)); err != nil {
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
			"NYLZO_REPORT_KEY": testReportKeyText,
			"NYLZO_DATA_DIR":   t.TempDir(),
			"GOMEMLIMIT":       testCase.operatorLimit,
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
	storage, _, err := openStore(directory, testDevelopment, sqlite.Open, slog.New(slog.DiscardHandler))
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

func TestSweepOnceNeedsNoPepperAndDropsOldReports(t *testing.T) {
	directory := t.TempDir()
	storage, _, err := openStore(directory, testDevelopment, sqlite.Open, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	storage.Close()
	reports := filepath.Join(directory, "abuse")
	if err = os.Mkdir(reports, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(reports, "2020-01-01"+abuse.FileExtension)
	if err = os.WriteFile(old, make([]byte, abuse.SealedLen), 0o600); err != nil {
		t.Fatal(err)
	}
	result := run(t, "", map[string]string{"NYLZO_DATA_DIR": directory}, nil, RunSweepOnce)
	if result.code != 0 {
		t.Fatalf("sweep-once without a pepper: exit %d, %s", result.code, result.stderr)
	}
	if strings.Count(result.stderr, `"level":"ERROR"`) != 0 {
		t.Fatalf("a clean sweep logged an error: %s", result.stderr)
	}
	if _, err = os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sweep-once kept an abuse report older than 7 days: %v", err)
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
			"NYLZO_DATA_DIR":   t.TempDir(),
			"NYLZO_PEPPER_FILE": pepperFile(t),
			"NYLZO_REPORT_KEY": testReportKeyText,
			"NYLZO_LISTEN":     taken.Addr().String(),
		},
		nil,
		RunServe,
	)
	if result.code != 1 || strings.Contains(result.stderr, `"msg":"sweep`) ||
		strings.Contains(result.stderr, `"msg":"listening"`) {
		t.Fatalf("a taken port: exit %d, %s", result.code, result.stderr)
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
	settings.DataDir = filepath.Join(t.TempDir(), "data")
	storage, _, err := openStore(settings.DataDir, testDevelopment, sqlite.Open, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	storage.Close()
	for _, directory := range []string{settings.DataDir, filepath.Join(settings.DataDir, disk.Directory), filepath.Join(settings.DataDir, disk.Directory, "tmp")} {
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
	storage, _, err := openStore(directory, testDevelopment, sqlite.Open, slog.New(slog.DiscardHandler))
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
	if result.code != 1 || !strings.Contains(result.stderr, "older schema") {
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
	if _, err = database.Exec(`PRAGMA user_version = 7`); err != nil {
		t.Fatal(err)
	}
	if result = run(t, "", map[string]string{"NYLZO_DATA_DIR": directory}, nil, RunSweepOnce); result.code != 1 ||
		!strings.Contains(result.stderr, "newer nylzo") {
		t.Fatalf("sweep-once on a newer schema: exit %d, %s", result.code, result.stderr)
	}
}
