//go:build !windows

package datadir

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSecureNarrowsWiderModesAndRefusesSymlinks(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(directory, "nylzo.db")
	blobs := filepath.Join(directory, "blobs")
	blob := filepath.Join(blobs, "tmp", "upload")
	for path, mode := range map[string]fs.FileMode{database: 0o644, blob: 0o640} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	layout := Layout{
		Directory: directory,
		Files:     []string{database, database + "-wal"},
		Trees:     []string{blobs, filepath.Join(directory, "abuse")},
	}
	narrowed, err := Secure(layout)
	if err != nil || narrowed != 5 {
		t.Fatalf("first run narrowed %d entries: %v", narrowed, err)
	}
	for path, want := range map[string]fs.FileMode{
		directory: 0o700, database: 0o600, blobs: 0o700, filepath.Dir(blob): 0o700, blob: 0o600,
	} {
		if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != want {
			t.Fatalf("%s: %v %v", path, info.Mode().Perm(), statErr)
		}
	}
	if narrowed, err = Secure(layout); err != nil || narrowed != 0 {
		t.Fatalf("second run narrowed %d entries: %v", narrowed, err)
	}
	if err = os.Symlink(database, filepath.Join(blobs, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err = Secure(layout); !errors.Is(err, ErrSymlink) {
		t.Fatalf("a symbolic link in blobs: %v", err)
	}
}
