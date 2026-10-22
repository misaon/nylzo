package main

import (
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
	env := map[string]string{"NYLZO_PEPPER": strings.Repeat("01", 32), "NYLZO_DATA_DIR": t.TempDir()}
	if _, err := serveConfig(func(k string) string { return env[k] }); err != nil {
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
	st, _, err := open(server.Config{DataDir: dir})
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
	t.Setenv("NYLZO_DATA_DIR", dir)
	if err := sweepOnce(slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("sweep-once ran against a missing store")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sweep-once created the data directory: %v", err)
	}
}
