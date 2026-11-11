//go:build darwin || linux

package privatefile

import (
	"os"
	"path/filepath"
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
	if private, checkErr := emptyPrivateDirectory(root); private || checkErr != nil {
		t.Fatalf("a directory with an entry passed as the new one: %v %v", private, checkErr)
	}
}
