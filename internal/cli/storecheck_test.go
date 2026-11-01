package cli

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/operator"
	"github.com/misaon/nylzo/internal/server/epoch"
)

type checked struct {
	code   int
	stdout string
	stderr string
}

func storeCheck(t *testing.T, dataDir, epochDir string) checked {
	t.Helper()
	variables := map[string]string{"NYLZO_DATA_DIR": dataDir, "NYLZO_EPOCH_DIR": epochDir}
	if runtime.GOOS != "linux" {
		variables["NYLZO_ALLOW_UNERASABLE_STORAGE"] = "1"
	}
	var stdout, stderr bytes.Buffer
	code := operator.RunStoreCheck(t.Context(), &command.Environment{
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &stderr,
		Getenv:   getenvFrom(variables),
		Terminal: noTerminal{},
		Version:  "test",
	}, nil)
	return checked{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// snapshot holds every name, mode and byte under the directory, so a check that writes anything shows.
func snapshot(t *testing.T, directory string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content := []byte{}
		if entry.Type().IsRegular() {
			if content, err = os.ReadFile(path); err != nil {
				return err
			}
		}
		entries[path] = fmt.Sprintf("%v %x", info.Mode(), content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestStoreCheckPassesAReleaseAndWritesNothing(t *testing.T) {
	directory := unpackFixture(t, releasedFixture)
	epochDir := filepath.Join(t.TempDir(), "epoch")
	if err := os.Mkdir(epochDir, 0o700); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, filepath.Dir(directory))
	result := storeCheck(t, directory, epochDir)
	if result.code != command.ExitOK || !strings.Contains(result.stdout, "upgrades it") ||
		!strings.Contains(result.stdout, "no seed") {
		t.Fatalf("store-check on a schema 5 directory: exit %d %q %s", result.code, result.stdout, result.stderr)
	}
	if after := snapshot(t, filepath.Dir(directory)); !maps.Equal(before, after) {
		t.Fatal("store-check changed the data directory")
	}
}

func TestStoreCheckRefusesWhatServeRefuses(t *testing.T) {
	for _, version := range []int{4, 6, 8} {
		t.Run(fmt.Sprintf("schema %d", version), func(t *testing.T) {
			directory := t.TempDir()
			database, err := sql.Open("sqlite", "file:"+filepath.Join(directory, "nylzo.db"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = database.Exec(
				fmt.Sprintf(`CREATE TABLE messages (locator BLOB); PRAGMA user_version = %d`, version),
			)
			if err = errors.Join(err, database.Close()); err != nil {
				t.Fatal(err)
			}
			epochDir := t.TempDir()
			before := snapshot(t, directory)
			if result := storeCheck(t, directory, epochDir); result.code != command.ExitError {
				t.Fatalf("store-check accepted schema %d: %q", version, result.stdout)
			}
			if after := snapshot(t, directory); !maps.Equal(before, after) {
				t.Fatal("store-check changed the data directory")
			}
		})
	}
	unreadable := map[string]func(t *testing.T, epochDir string){
		"a missing epoch directory": func(t *testing.T, epochDir string) {
			t.Helper()
			if err := os.Remove(epochDir); err != nil {
				t.Fatal(err)
			}
		},
		"a seed others can read": func(t *testing.T, epochDir string) {
			t.Helper()
			if err := os.WriteFile(
				filepath.Join(epochDir, epoch.FileName),
				make([]byte, epoch.FileLen),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
		},
		"an epoch directory nobody can read": func(t *testing.T, epochDir string) {
			t.Helper()
			if os.Getuid() == 0 {
				t.Skip("root reads every directory")
			}
			if err := os.WriteFile(
				filepath.Join(epochDir, epoch.FileName),
				make([]byte, epoch.FileLen),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(epochDir, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(epochDir, 0o700) })
		},
	}
	for name, prepare := range unreadable {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("Windows has no POSIX modes")
			}
			epochDir := filepath.Join(t.TempDir(), "epoch")
			if err := os.Mkdir(epochDir, 0o700); err != nil {
				t.Fatal(err)
			}
			prepare(t, epochDir)
			if result := storeCheck(t, t.TempDir(), epochDir); result.code != command.ExitError {
				t.Fatalf("store-check accepted %s: %q", name, result.stdout)
			}
		})
	}
}
