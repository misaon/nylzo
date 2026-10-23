package cli

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

	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestSweepOnceNeedsAnExistingStore(t *testing.T) {
	dir := t.TempDir()
	if err := requireStore(dir); err == nil {
		t.Fatal("sweep-once accepted an empty data directory")
	}
	if err := os.WriteFile(filepath.Join(dir, dbFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireStore(dir); err == nil {
		t.Fatal("sweep-once accepted a store without a blob directory")
	}
	if err := os.Mkdir(filepath.Join(dir, disk.Dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := requireStore(dir); err != nil {
		t.Fatalf("sweep-once refused an existing store: %v", err)
	}
}

func TestShutdownPastItsTimeoutIsAnOrderlyExit(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{})
	hs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	})}
	served := make(chan error, 1)
	go func() { served <- hs.Serve(ln) }()
	go http.Get("http://" + ln.Addr().String())
	<-entered
	if err := shutdown(hs, 50*time.Millisecond, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("a shutdown that outlives its timeout must still end cleanly: %v", err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("server still serving: %v", err)
	}
}

func TestServeConfigSetsASoftMemoryLimit(t *testing.T) {
	previous := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(previous) })
	debug.SetMemoryLimit(math.MaxInt64)
	env := map[string]string{"NYLZO_PEPPER": testPepper, "NYLZO_DATA_DIR": t.TempDir()}
	if _, err := serveConfig(Env{Getenv: func(k string) string { return env[k] }}); err != nil {
		t.Fatal(err)
	}
	if got := debug.SetMemoryLimit(-1); got != defaultMemoryLimit {
		t.Fatalf("serve must set the soft memory limit, got %d", got)
	}
}

func TestMemoryLimitYieldsToGOMEMLIMIT(t *testing.T) {
	previous := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(previous) })
	debug.SetMemoryLimit(math.MaxInt64)
	applyMemoryLimit(func(string) string { return "" })
	if got := debug.SetMemoryLimit(-1); got != defaultMemoryLimit {
		t.Fatalf("without GOMEMLIMIT the limit must be %d, got %d", defaultMemoryLimit, got)
	}
	debug.SetMemoryLimit(math.MaxInt64)
	applyMemoryLimit(func(k string) string { return map[string]string{"GOMEMLIMIT": "300MiB"}[k] })
	if got := debug.SetMemoryLimit(-1); got != math.MaxInt64 {
		t.Fatalf("an operator GOMEMLIMIT must win, got %d", got)
	}
}

func TestRequireStoreAcceptsWhatOpenCreates(t *testing.T) {
	dir := t.TempDir()
	st, _, err := open(server.Config{DataDir: dir}, sqlite.Open)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	if err := requireStore(dir); err != nil {
		t.Fatalf("sweep-once must accept a store that serve created: %v", err)
	}
}

func TestSweepOnceLeavesAMissingStoreAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	if err := sweepOnce(env(map[string]string{"NYLZO_DATA_DIR": dir}), slog.New(slog.DiscardHandler)); !errors.Is(err, errNoStore) {
		t.Fatalf("sweep-once ran against a missing store: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sweep-once created the data directory: %v", err)
	}
}

var testPepper = strings.Repeat("0123456789abcdef", 4)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func runCLI(args []string, values map[string]string) (int, string, string) {
	var stdout, stderr strings.Builder
	code := Run(args, Env{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Getenv: env(values), Terminal: noTerminal{}})
	return code, stdout.String(), stderr.String()
}

func TestRunAnswersUsageErrorsWithoutRunning(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	if code, stdout, stderr := runCLI([]string{"serve", "--help"}, nil); code != 0 || stdout != "" || !strings.HasPrefix(stderr, "usage: nylzo serve") {
		t.Fatalf("serve --help: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, args := range [][]string{nil, {"sweep-once", "--dry-run"}, {"serve", "extra"}, {"bogus"}, {"version", "extra"}} {
		code, stdout, stderr := runCLI(args, map[string]string{"NYLZO_DATA_DIR": dir, "NYLZO_PEPPER": testPepper})
		if code != 2 || stdout != "" || !strings.Contains(stderr, "nylzo") {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a usage error still ran a command")
	}
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		if code, stdout, stderr := runCLI(args, nil); code != 0 || !strings.HasPrefix(stdout, "usage: nylzo ") || stderr != "" {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
	if code, stdout, _ := runCLI([]string{"version"}, nil); code != 0 || strings.TrimSpace(stdout) == "" {
		t.Fatalf("version: exit %d, %q", code, stdout)
	}
}

func TestConfigErrorsNameTheVariable(t *testing.T) {
	code, _, stderr := runCLI([]string{"sweep-once"}, map[string]string{"NYLZO_RATE_SOURCE_BURST": "0.5"})
	if code != 1 || !strings.Contains(stderr, "NYLZO_RATE_SOURCE_BURST must be at least 1") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestSweepOnceNeedsNoPepperAndDropsOldReports(t *testing.T) {
	dir := t.TempDir()
	st, _, err := open(server.Config{DataDir: dir}, sqlite.Open)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	reports := filepath.Join(dir, "abuse")
	if err := os.Mkdir(reports, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(reports, "2020-01-01.jsonl")
	if err := os.WriteFile(old, []byte(`{"date":"2020-01-01","id":"x","reason":""}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI([]string{"sweep-once"}, map[string]string{"NYLZO_DATA_DIR": dir})
	if code != 0 {
		t.Fatalf("sweep-once without a pepper: exit %d, %s", code, stderr)
	}
	if strings.Count(stderr, `"level":"ERROR"`) != 0 {
		t.Fatalf("a clean sweep logged an error: %s", stderr)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sweep-once kept an abuse report older than 7 days: %v", err)
	}
}

func TestServeChecksThePortBeforeItSweeps(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	code, _, stderr := runCLI([]string{"serve"}, map[string]string{"NYLZO_DATA_DIR": t.TempDir(), "NYLZO_PEPPER": testPepper, "NYLZO_LISTEN": taken.Addr().String()})
	if code != 1 || strings.Contains(stderr, `"msg":"sweep`) || strings.Contains(stderr, `"msg":"listening"`) {
		t.Fatalf("a taken port: exit %d, %s", code, stderr)
	}
}

func TestLogLinesOfTheCommandCarryTheMinuteOnly(t *testing.T) {
	_, _, stderr := runCLI([]string{"sweep-once"}, map[string]string{"NYLZO_DATA_DIR": filepath.Join(t.TempDir(), "absent")})
	var line struct {
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal([]byte(stderr), &line); err != nil || line.Time.IsZero() || !line.Time.Equal(line.Time.Truncate(time.Minute)) {
		t.Fatalf("a log line of nylzo carries a time finer than the minute: %q %v", stderr, err)
	}
}

func TestOpenCreatesPrivateDirectories(t *testing.T) {
	cfg := server.Defaults()
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	st, _, err := open(cfg, sqlite.Open)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	for _, dir := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, disk.Dir), filepath.Join(cfg.DataDir, disk.Dir, "tmp")} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is open to other users: %v", dir, info.Mode().Perm())
		}
	}
}

func TestSweepOnceLeavesAnOlderSchemaToServe(t *testing.T) {
	dir := t.TempDir()
	st, _, err := open(server.Config{DataDir: dir}, sqlite.Open)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, dbFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 3`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	code, _, stderr := runCLI([]string{"sweep-once"}, map[string]string{"NYLZO_DATA_DIR": dir})
	if code != 1 || !strings.Contains(stderr, "older schema") {
		t.Fatalf("sweep-once on an older schema: exit %d, %s", code, stderr)
	}
	db, err = sql.Open("sqlite", "file:"+filepath.Join(dir, dbFile))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 3 {
		t.Fatalf("sweep-once migrated the database under a running older server: version %d %v", version, err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runCLI([]string{"sweep-once"}, map[string]string{"NYLZO_DATA_DIR": dir}); code != 1 || !strings.Contains(stderr, "newer nylzo") {
		t.Fatalf("sweep-once on a newer schema: exit %d, %s", code, stderr)
	}
}
