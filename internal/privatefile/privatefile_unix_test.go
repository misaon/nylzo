//go:build darwin || linux

package privatefile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func shareWithEveryone(t *testing.T, directory string) {
	t.Helper()
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	shareByACL(t, directory)
}

func requirePrivate(t *testing.T, path string, directory bool) {
	t.Helper()
	want := os.FileMode(0o600)
	if directory {
		want = os.ModeDir | 0o700
	}
	if info, err := os.Lstat(path); err != nil || info.Mode() != want {
		t.Fatalf("%s: %v %v, want %v", path, info.Mode(), err, want)
	}
	requireNoACL(t, path)
}

func TestReadbackRefusesAnEntryOthersCanOpen(t *testing.T) {
	t.Parallel()
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.WriteFile(shared, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(shared)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if privateEntry(file) {
		t.Fatal("the readback accepted a file that group and others can read")
	}
}

func TestMkdirTempRefusesADirectoryThatIsNotEmpty(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "taken")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "entry"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if private, checkErr := emptyPrivateDirectory(root, privateEntry); private || checkErr != nil {
		t.Fatalf("a directory with an entry passed as the new one: %v %v", private, checkErr)
	}
}

func TestAnEntryThatFailsTheReadbackIsRemoved(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	refused := func(*os.File) bool { return false }
	if _, err := createChecked(filepath.Join(parent, "file"), refused); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Create: %v", err)
	}
	base, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if _, err = createInChecked(base, "entry", refused); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("CreateIn: %v", err)
	}
	if _, err = mkdirTempChecked(parent, "nylzo-", refused); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("MkdirTemp: %v", err)
	}
	requireEmpty(t, parent)
}

// The umask belongs to the whole process, so this test must not run in parallel.
func TestMkdirTempRemovesADirectoryItCannotOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a directory whatever its mode")
	}
	parent := t.TempDir()
	for _, mask := range []int{0o700, 0o300} {
		previous := syscall.Umask(mask)
		root, err := MkdirTemp(parent, "nylzo-")
		syscall.Umask(previous)
		if err == nil {
			root.Close()
			t.Fatalf("umask %o: a directory the process cannot open was accepted", mask)
		}
	}
	requireEmpty(t, parent)
}

func TestMkdirTempRefusesAParentItCannotUse(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	if _, err := MkdirTemp(filepath.Join(parent, "missing"), "nylzo-"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing parent: %v", err)
	}
	if os.Geteuid() == 0 {
		return
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o700) })
	if _, err := MkdirTemp(parent, "nylzo-"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("a read-only parent: %v", err)
	}
}

func TestReadbackRefusesAClosedEntry(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	file, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if privateEntry(file) || enforcedLocally(file) {
		t.Fatal("the readback accepted an entry it could not read")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	if private, checkErr := emptyPrivateDirectory(root, privateEntry); private || checkErr == nil {
		t.Fatalf("a closed directory passed as the new one: %v %v", private, checkErr)
	}
}

func requireEmpty(t *testing.T, directory string) {
	t.Helper()
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
		t.Fatalf("a refused entry stayed: %v %v", entries, err)
	}
}
