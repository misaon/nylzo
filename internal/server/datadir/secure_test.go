//go:build !windows

package datadir

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
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
		Trees:     []string{blobs, filepath.Join(directory, "absent")},
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

func TestHoldLetsOneProcessServeADataDirectory(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "data")
	release, err := Hold(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Hold(directory); !errors.Is(err, ErrServed) {
		t.Fatalf("a second hold on a served data directory: %v", err)
	}
	release()
	release, err = Hold(directory)
	if err != nil {
		t.Fatalf("a hold after the release: %v", err)
	}
	release()
}

func foreignDirectory(t *testing.T) string {
	t.Helper()
	if os.Getuid() != 0 {
		return "/"
	}
	directory := t.TempDir()
	if err := os.Chown(directory, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestCheckRefusesALayoutItCannotSecure(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	regular := filepath.Join(directory, "regular")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	underRegular := filepath.Join(regular, "inside")
	for name, test := range map[string]struct {
		layout Layout
		want   error
	}{
		"missing directory":       {Layout{Directory: filepath.Join(directory, "absent")}, fs.ErrNotExist},
		"foreign directory":       {Layout{Directory: foreignDirectory(t)}, ErrForeignOwner},
		"file under a file":       {Layout{Directory: directory, Files: []string{underRegular}}, syscall.ENOTDIR},
		"file is a symbolic link": {Layout{Directory: directory, Files: []string{link}}, ErrSymlink},
		"tree under a file":       {Layout{Directory: directory, Trees: []string{underRegular}}, syscall.ENOTDIR},
	} {
		if err := Check(test.layout); !errors.Is(err, test.want) {
			t.Errorf("%s: %v, want %v", name, err, test.want)
		}
	}
}

type failingEntry struct {
	fs.DirEntry

	err error
}

func (e failingEntry) Info() (fs.FileInfo, error) {
	return nil, e.err
}

func TestSecureSkipsTreeEntriesThatVanishWhileItWalks(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	regular := filepath.Join(directory, "regular")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	vanished := filepath.Join(directory, "vanished")
	if err := os.WriteFile(vanished, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(vanished, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(vanished)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(vanished); err != nil {
		t.Fatal(err)
	}
	narrowed := 0
	walked := secureWalked(&narrowed)
	if err = walked(vanished, failingEntry{err: fs.ErrNotExist}, nil); err != nil {
		t.Errorf("an entry gone before its information was read: %v", err)
	}
	if err = walked(vanished, fs.FileInfoToDirEntry(info), nil); err != nil {
		t.Errorf("an entry gone before its mode was narrowed: %v", err)
	}
	if narrowed != 0 {
		t.Errorf("%d vanished entries counted as narrowed", narrowed)
	}
	if err = walked(vanished, failingEntry{err: fs.ErrPermission}, nil); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("an entry whose information could not be read: %v", err)
	}
	if err = walked(
		filepath.Join(regular, "inside"),
		fs.FileInfoToDirEntry(info),
		nil,
	); !errors.Is(
		err,
		syscall.ENOTDIR,
	) {
		t.Errorf("an entry whose mode could not be narrowed: %v", err)
	}
}

func TestHoldRefusesADirectoryItCannotOpen(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	regular := filepath.Join(directory, "regular")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Hold(filepath.Join(regular, "data")); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("a data directory under a file: %v", err)
	}
	if os.Getuid() == 0 {
		return
	}
	closed := filepath.Join(directory, "closed")
	if err := os.Mkdir(closed, 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := Hold(closed); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("a data directory the process may not read: %v", err)
	}
}
