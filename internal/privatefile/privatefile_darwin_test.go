package privatefile

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shareByACL gives everyone an inheritable ACE and proves that a file made the usual way there inherits it.
func shareByACL(t *testing.T, directory string) {
	t.Helper()
	grant := "everyone allow read,write,list,add_file,search,file_inherit,directory_inherit"
	if output, err := exec.CommandContext(t.Context(), "/bin/chmod", "+a", grant, directory).
		CombinedOutput(); err != nil {
		t.Fatalf("chmod +a: %v %s", err, output)
	}
	usual := filepath.Join(directory, "usual")
	if err := os.WriteFile(usual, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if entries := aclEntries(t, usual); len(entries) == 0 {
		t.Fatal("a file made with os.WriteFile did not inherit the ACE")
	}
}

func requireNoACL(t *testing.T, path string) {
	t.Helper()
	if entries := aclEntries(t, path); len(entries) != 0 {
		t.Fatalf("%s carries ACL entries: %v", path, entries)
	}
}

func aclEntries(t *testing.T, path string) []string {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "/bin/ls", "-led", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var entries []string
	for line := range strings.Lines(string(output)) {
		if strings.HasPrefix(strings.TrimSpace(line), "0:") || strings.Contains(line, " allow ") {
			entries = append(entries, strings.TrimSpace(line))
		}
	}
	return entries
}

func TestCreateInRefusesAndRemovesAFileThatInheritsAnACE(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	shareByACL(t, directory)
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if file, createErr := CreateIn(root, "new"); !errors.Is(createErr, ErrNotPrivate) {
		file.Close()
		t.Fatalf("CreateIn in a directory that passes an ACE on: %v", createErr)
	}
	if _, err = os.Lstat(filepath.Join(directory, "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the refused file was left behind: %v", err)
	}
}
