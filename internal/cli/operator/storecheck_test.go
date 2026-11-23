package operator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestStoreCheckPassesADataDirectoryThatServeCreates(t *testing.T) {
	t.Parallel()
	absent := filepath.Join(t.TempDir(), "absent")
	result := run(t, "", map[string]string{"NYLZO_DATA_DIR": absent}, nil, RunStoreCheck)
	if result.code != command.ExitOK {
		t.Fatalf("store-check without a data directory: exit %d, %s", result.code, result.stderr)
	}
	if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store-check created the data directory: %v", err)
	}
}

func TestStoreCheckRefusesWhatServeWouldRefuse(t *testing.T) {
	t.Parallel()
	linkedLog := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "log"), filepath.Join(linkedLog, databaseFile+"-wal")); err != nil {
		t.Fatal(err)
	}
	for name, variables := range map[string]map[string]string{
		"an invalid setting": {"NYLZO_DATA_DIR": t.TempDir(), "NYLZO_RATE_SOURCE_BURST": "0.5"},
		"a linked log":       {"NYLZO_DATA_DIR": linkedLog},
		"unerasable storage": {
			"NYLZO_DATA_DIR":                 unerasableDirectory(t),
			"NYLZO_ALLOW_UNERASABLE_STORAGE": "0",
		},
	} {
		if result := run(t, "", variables, nil, RunStoreCheck); result.code != command.ExitError {
			t.Errorf("store-check passed %s: %s", name, result.stdout)
		}
	}
}

func TestStoreCheckRefusesAnEpochDirectoryOnUnerasableStorage(t *testing.T) {
	t.Parallel()
	settings := config.Server{DataDirectory: t.TempDir(), EpochDirectoryOverride: unerasableDirectory(t)}
	var output bytes.Buffer
	err := checkEpochDirectory(&settings, &output, slog.New(slog.DiscardHandler))
	if !errors.Is(err, erase.ErrNotInPlace) {
		t.Fatalf("an epoch directory on unerasable storage: %v", err)
	}
}

func TestStoreCheckRefusesAStateItDoesNotKnow(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	for _, readiness := range []sqlite.Readiness{sqlite.ReadinessCreate, sqlite.ReadinessOpen, sqlite.ReadinessUpgrade} {
		if err := describeReadiness(readiness, &output); err != nil {
			t.Errorf("readiness %d: %v", readiness, err)
		}
	}
	for _, state := range []epoch.State{epoch.StateNoSeed, epoch.StateSeed, epoch.StateReleasedSeed} {
		if err := describeEpochState(state, &output); err != nil {
			t.Errorf("epoch state %d: %v", state, err)
		}
	}
	if strings.Count(output.String(), "\n") != 6 {
		t.Errorf("every known state must print one line: %q", output.String())
	}
	if err := describeReadiness(sqlite.Readiness(99), &output); !errors.Is(err, errUnknownReadiness) {
		t.Errorf("an unknown readiness: %v", err)
	}
	if err := describeEpochState(epoch.State(99), &output); !errors.Is(err, errUnknownEpochState) {
		t.Errorf("an unknown epoch state: %v", err)
	}
}

func TestStoreCheckPassesAStoreThatServeLeft(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	createStore(t, directory)
	settings := config.Server{DataDirectory: directory, Development: testDevelopment}
	if err := prepareEpoch(&settings, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if result := run(t, "", map[string]string{"NYLZO_DATA_DIR": directory}, nil, RunStoreCheck); result.code !=
		command.ExitOK {
		t.Fatalf("store-check on a store that serve left: exit %d, %s", result.code, result.stderr)
	}
}

func TestStoreCheckRefusesADamagedStore(t *testing.T) {
	t.Parallel()
	damagedDatabase := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(damagedDatabase, databaseFile),
		bytes.Repeat([]byte("damaged"), 1000),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	damagedSeed := t.TempDir()
	if err := os.Mkdir(filepath.Join(damagedSeed, "epoch"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(damagedSeed, "epoch", epoch.FileName), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	leftover := t.TempDir()
	if err := os.Mkdir(filepath.Join(leftover, "epoch"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, variables := range map[string]map[string]string{
		"a damaged database": {"NYLZO_DATA_DIR": damagedDatabase},
		"a damaged seed":     {"NYLZO_DATA_DIR": damagedSeed},
		"a leftover epoch":   {"NYLZO_DATA_DIR": leftover, "NYLZO_EPOCH_DIR": t.TempDir()},
	} {
		if result := run(t, "", variables, nil, RunStoreCheck); result.code != command.ExitError {
			t.Errorf("store-check passed %s: %s", name, result.stdout)
		}
	}
}

func TestTheServerCommandsExplainThemselves(t *testing.T) {
	t.Parallel()
	for name, operatorCommand := range map[string]func(context.Context, *command.Environment, []string) int{
		"sweep-once": RunSweepOnce,
		"takedown":   RunTakedown,
	} {
		var stdout bytes.Buffer
		environment := &command.Environment{Stdout: &stdout, Stderr: new(bytes.Buffer), Getenv: getenvFrom(nil)}
		if code := operatorCommand(t.Context(), environment, []string{"-h"}); code != command.ExitOK ||
			!strings.Contains(stdout.String(), name) {
			t.Errorf("%s -h: exit %d, %s", name, code, stdout.String())
		}
	}
}
