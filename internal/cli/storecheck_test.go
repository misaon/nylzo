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
	"github.com/misaon/nylzo/internal/epoch"
)

type checked struct {
	code   int
	stdout string
	stderr string
}

func storeCheck(t *testing.T, dataDirectory, epochDirectory string) checked {
	t.Helper()
	variables := map[string]string{"NYLZO_DATA_DIR": dataDirectory, "NYLZO_EPOCH_DIR": epochDirectory}
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
	epochDirectory := filepath.Join(t.TempDir(), "epoch")
	if err := os.Mkdir(epochDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, filepath.Dir(directory))
	result := storeCheck(t, directory, epochDirectory)
	if result.code != command.ExitOK || !strings.Contains(result.stdout, "upgrades it") ||
		!strings.Contains(result.stdout, "no seed") {
		t.Fatalf("store-check on a schema 5 directory: exit %d %q %s", result.code, result.stdout, result.stderr)
	}
	if after := snapshot(t, filepath.Dir(directory)); !maps.Equal(before, after) {
		t.Fatal("store-check changed the data directory")
	}
	legacySeed := make([]byte, 4096)
	copy(legacySeed, "nylzo-e1\x09")
	legacySeed[31] = 1
	legacySeed[40] = 1
	if err := os.WriteFile(filepath.Join(epochDirectory, epoch.FileName), legacySeed, 0o600); err != nil {
		t.Fatal(err)
	}
	beforeEpoch := snapshot(t, epochDirectory)
	result = storeCheck(t, directory, epochDirectory)
	if result.code != command.ExitOK || !strings.Contains(result.stdout, "seed of an earlier release") {
		t.Fatalf(
			"store-check on the seed of an earlier release: exit %d %q %s",
			result.code,
			result.stdout,
			result.stderr,
		)
	}
	if after := snapshot(t, epochDirectory); !maps.Equal(beforeEpoch, after) {
		t.Fatal("store-check changed the epoch directory")
	}
}

func TestStoreCheckRefusesWhatServeRefuses(t *testing.T) {
	for _, version := range []int{4, 6, 9} {
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
			epochDirectory := t.TempDir()
			before := snapshot(t, directory)
			if result := storeCheck(t, directory, epochDirectory); result.code != command.ExitError {
				t.Fatalf("store-check accepted schema %d: %q", version, result.stdout)
			}
			if after := snapshot(t, directory); !maps.Equal(before, after) {
				t.Fatal("store-check changed the data directory")
			}
		})
	}
	unreadable := map[string]func(t *testing.T, dataDirectory, epochDirectory string){
		"a missing epoch directory": func(t *testing.T, _, epochDirectory string) {
			t.Helper()
			if err := os.Remove(epochDirectory); err != nil {
				t.Fatal(err)
			}
		},
		"a seed others can read": func(t *testing.T, _, epochDirectory string) {
			t.Helper()
			if err := os.WriteFile(
				filepath.Join(epochDirectory, epoch.FileName),
				make([]byte, epoch.FileLen),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
		},
		"an epoch directory nobody can read": func(t *testing.T, _, epochDirectory string) {
			t.Helper()
			if os.Getuid() == 0 {
				t.Skip("root reads every directory")
			}
			if err := os.WriteFile(
				filepath.Join(epochDirectory, epoch.FileName),
				make([]byte, epoch.FileLen),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(epochDirectory, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(epochDirectory, 0o700) })
		},
		"a seed left in the data directory": func(t *testing.T, dataDirectory, _ string) {
			t.Helper()
			leftover := filepath.Join(dataDirectory, "epoch")
			if err := os.Mkdir(leftover, 0o700); err != nil {
				t.Fatal(err)
			}
			seed := filepath.Join(leftover, epoch.FileName)
			if err := os.WriteFile(seed, make([]byte, epoch.FileLen), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, prepare := range unreadable {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("Windows has no POSIX modes")
			}
			dataDirectory, epochDirectory := t.TempDir(), filepath.Join(t.TempDir(), "epoch")
			if err := os.Mkdir(epochDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			prepare(t, dataDirectory, epochDirectory)
			if result := storeCheck(t, dataDirectory, epochDirectory); result.code != command.ExitError {
				t.Fatalf("store-check accepted %s: %q", name, result.stdout)
			}
		})
	}
}
