package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
)

// Readiness is what serve of this build would do with a database.
type Readiness int

const (
	ReadinessCreate Readiness = iota
	ReadinessOpen
	ReadinessUpgrade
)

var errLogAhead = errors.New(
	"sqlite: the database file holds no schema yet but its log holds changes; stop the server and check again",
)

// Inspect reads the schema version without writing anything, not even the log or its index: immutable mode reads
// only the database file, so a schema that lives in the log alone is refused instead of guessed.
func Inspect(ctx context.Context, path string) (Readiness, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return ReadinessCreate, nil
	} else if err != nil {
		return ReadinessCreate, err
	}
	database, err := sql.Open("sqlite", "file:"+(&url.URL{Path: path}).EscapedPath()+"?mode=ro&immutable=1")
	if err != nil {
		return ReadinessCreate, err
	}
	defer database.Close()
	version, err := userVersion(ctx, database)
	if err != nil {
		return ReadinessCreate, fmt.Errorf("sqlite: read the schema version: %w", err)
	}
	var objects int
	if err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master`).Scan(&objects); err != nil {
		return ReadinessCreate, fmt.Errorf("sqlite: read the schema: %w", err)
	}
	switch {
	case version == 0 && objects == 0:
		return emptyReadiness(path)
	case version == releasedSchema:
		return ReadinessUpgrade, nil
	}
	if versionErr := checkVersion(version); versionErr != nil {
		return ReadinessCreate, versionErr
	}
	return ReadinessOpen, nil
}

func emptyReadiness(path string) (Readiness, error) {
	info, err := os.Stat(path + "-wal")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ReadinessCreate, nil
	case err != nil:
		return ReadinessCreate, err
	case info.Size() > 0:
		return ReadinessCreate, errLogAhead
	}
	return ReadinessCreate, nil
}
