package operator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/datadir"
	"github.com/misaon/nylzo/internal/server/epoch"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

// RunStoreCheck tells, without writing anything, whether serve of this build can open the data directory, so an
// updater can refuse a build before it stops the running one.
func RunStoreCheck(_ context.Context, environment *command.Environment, arguments []string) int {
	return runServer(
		environment,
		arguments,
		"store-check",
		func(logger *slog.Logger) error { return storeCheck(environment.Getenv, environment.Stdout, logger) },
	)
}

func storeCheck(getenv func(string) string, output io.Writer, logger *slog.Logger) error {
	settings, err := config.FromEnv(getenv, false)
	if err != nil {
		return err
	}
	if dataErr := checkDataDirectory(&settings, output, logger); dataErr != nil {
		return dataErr
	}
	return checkEpochDirectory(&settings, output, logger)
}

func checkDataDirectory(settings *config.Server, output io.Writer, logger *slog.Logger) error {
	dataDir := settings.DataDir
	if _, err := os.Stat(dataDir); errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(output, "store: no data directory yet; serve creates it")
		return nil
	}
	databasePath := filepath.Join(dataDir, databaseFile)
	err := datadir.Check(datadir.Layout{
		Directory: dataDir,
		Files:     []string{databasePath, databasePath + "-wal", databasePath + "-shm"},
		Trees:     []string{filepath.Join(dataDir, disk.Directory), abuse.Directory(dataDir)},
	})
	if err != nil {
		return err
	}
	paths := []string{dataDir}
	if len(existing(filepath.Join(dataDir, disk.Directory, "tmp"))) > 0 {
		if paths, err = erasablePaths(dataDir); err != nil {
			return err
		}
	}
	if erasableErr := requireErasable(paths, settings.Development, logger); erasableErr != nil {
		return erasableErr
	}
	readiness, err := sqlite.Inspect(context.Background(), databasePath)
	if err != nil {
		return err
	}
	switch readiness {
	case sqlite.ReadinessOpen:
		fmt.Fprintln(output, "store: this build opens it")
	case sqlite.ReadinessUpgrade:
		fmt.Fprintln(output, "store: written by nylzo 0.1 or 0.2; serve upgrades it and erases every message in it")
	case sqlite.ReadinessCreate:
		fmt.Fprintln(output, "store: no database yet; serve creates it")
	default:
		return fmt.Errorf("store-check: readiness %d is unknown", readiness)
	}
	return nil
}

func checkEpochDirectory(settings *config.Server, output io.Writer, logger *slog.Logger) error {
	directory := settings.EpochDirectory()
	state, err := epoch.Inspect(directory)
	switch {
	case errors.Is(err, epoch.ErrNoDirectory) && settings.EpochDir == "":
		fmt.Fprintln(output, "epoch: no directory yet; serve creates it in the data directory")
		return nil
	case err != nil:
		return err
	}
	if erasableErr := requireErasable(
		epoch.ErasablePaths(directory),
		settings.Development,
		logger,
	); erasableErr != nil {
		return erasableErr
	}
	if state == epoch.StateSeed {
		fmt.Fprintln(output, "epoch: a seed; serve goes on with it")
	} else {
		fmt.Fprintln(output, "epoch: no seed; serve starts a new one, which ends every live message")
	}
	return nil
}

func existing(paths ...string) []string {
	var found []string
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			found = append(found, path)
		}
	}
	return found
}
