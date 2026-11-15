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
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/datadir"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var (
	errUnknownReadiness  = errors.New("store-check: readiness")
	errUnknownEpochState = errors.New("store-check: epoch state")
)

// RunStoreCheck tells, without writing anything, whether serve of this build can open the data directory, so an
// updater can refuse a build before it stops the running one.
func RunStoreCheck(ctx context.Context, environment *command.Environment, arguments []string) int {
	return runServer(ctx, environment, arguments, "store-check", func(ctx context.Context, logger *slog.Logger) error {
		return storeCheck(ctx, environment.Getenv, environment.Stdout, logger)
	})
}

func storeCheck(ctx context.Context, getenv func(string) string, output io.Writer, logger *slog.Logger) error {
	if !serverSupported {
		return errServerUnsupported
	}
	settings, err := config.FromEnvironment(getenv, false)
	if err != nil {
		return err
	}
	if dataErr := checkDataDirectory(ctx, &settings, output, logger); dataErr != nil {
		return dataErr
	}
	reports, err := findReports(settings.DataDirectory, settings.Development, logger)
	if err != nil {
		return err
	}
	if reports.found {
		fmt.Fprintln(output, "reports: abuse reports of an earlier release; serve erases them")
	}
	return checkEpochDirectory(&settings, output, logger)
}

func checkDataDirectory(ctx context.Context, settings *config.Server, output io.Writer, logger *slog.Logger) error {
	dataDirectory := settings.DataDirectory
	if _, err := os.Stat(dataDirectory); errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(output, "store: no data directory yet; serve creates it")
		return nil
	}
	databasePath := filepath.Join(dataDirectory, databaseFile)
	err := datadir.Check(datadir.Layout{
		Directory: dataDirectory,
		Files:     []string{databasePath, databasePath + "-wal", databasePath + "-shm"},
		Trees:     []string{filepath.Join(dataDirectory, disk.Directory)},
	})
	if err != nil {
		return err
	}
	paths := []string{dataDirectory}
	if _, statErr := os.Stat(filepath.Join(dataDirectory, disk.Directory, "tmp")); statErr == nil {
		if paths, err = erasablePaths(dataDirectory); err != nil {
			return err
		}
	}
	if erasableErr := requireErasable(paths, settings.Development, logger); erasableErr != nil {
		return erasableErr
	}
	readiness, err := sqlite.Inspect(ctx, databasePath)
	if err != nil {
		return err
	}
	switch readiness {
	case sqlite.ReadinessOpen:
		fmt.Fprintln(output, "store: this build opens it")
	case sqlite.ReadinessUpgrade:
		fmt.Fprintln(
			output,
			"store: written by a release before 0.14.0; serve upgrades it and erases every message in it",
		)
	case sqlite.ReadinessCreate:
		fmt.Fprintln(output, "store: no database yet; serve creates it")
	default:
		return fmt.Errorf("%w %d is unknown", errUnknownReadiness, readiness)
	}
	return nil
}

func checkEpochDirectory(settings *config.Server, output io.Writer, logger *slog.Logger) error {
	directory := settings.EpochDirectory()
	state, err := inspectEpoch(directory)
	switch {
	case errors.Is(err, epoch.ErrNoDirectory) && settings.EpochDirectoryOverride == "":
		fmt.Fprintln(output, "epoch: no directory yet; serve creates it in the data directory")
		return nil
	case err != nil:
		return err
	}
	if leftoverErr := server.RefuseEpochInDataDirectory(settings); leftoverErr != nil {
		return leftoverErr
	}
	if erasableErr := requireErasable(
		epoch.ErasablePaths(directory),
		settings.Development,
		logger,
	); erasableErr != nil {
		return erasableErr
	}
	switch state {
	case epoch.StateSeed:
		fmt.Fprintln(output, "epoch: a seed; serve goes on with it")
	case epoch.StateReleasedSeed:
		fmt.Fprintln(
			output,
			"epoch: a seed of a release before 0.14.0; serve erases it with every message of that release "+
				"and starts a new one",
		)
	case epoch.StateNoSeed:
		fmt.Fprintln(output, "epoch: no seed; serve starts a new one, which ends every live message")
	default:
		return fmt.Errorf("%w %d is unknown", errUnknownEpochState, state)
	}
	return nil
}
