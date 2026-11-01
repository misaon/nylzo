package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestInspectMissingAndEmptyDatabasesWithoutCreatingFiles(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	if readiness, err := Inspect(t.Context(), path); err != nil || readiness != ReadinessCreate {
		t.Fatalf("missing database: %v %v", readiness, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspection created a database: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, contents := range [][]byte{nil, []byte("uncheckpointed changes")} {
		if err := os.WriteFile(path+"-wal", contents, 0o600); err != nil {
			t.Fatal(err)
		}
		readiness, err := Inspect(t.Context(), path)
		if len(contents) == 0 {
			if err != nil || readiness != ReadinessCreate {
				t.Fatalf("empty database: %v %v", readiness, err)
			}
		} else if !errors.Is(err, errLogAhead) {
			t.Fatalf("inspection guessed a schema from a nonempty WAL: %v", err)
		}
	}
}

func TestInspectRecognizesReleasedAndUnsupportedSchemasReadOnly(t *testing.T) {
	t.Parallel()
	for _, version := range []int{releasedSchema, 0, 9999} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "nylzo.db")
			database, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			statement := fmt.Sprintf("CREATE TABLE fixture (value TEXT); PRAGMA user_version=%d", version)
			if _, err = database.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if err = database.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			readiness, err := Inspect(t.Context(), path)
			if version == releasedSchema {
				if err != nil || readiness != ReadinessUpgrade {
					t.Fatalf("released schema: %v %v", readiness, err)
				}
			} else if err == nil {
				t.Fatalf("unsupported schema %d was accepted", version)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatal("inspection modified the database")
			}
			for _, suffix := range []string{"-wal", "-shm"} {
				if _, statErr := os.Stat(path + suffix); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("inspection created %s: %v", suffix, statErr)
				}
			}
		})
	}
}

func TestInspectRefusesCorruptFilesAndCancelledContexts(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "broken.db")
	if err := os.WriteFile(path, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(t.Context(), path); err == nil {
		t.Fatal("corrupt database was accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Inspect(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspection: %v", err)
	}
}
