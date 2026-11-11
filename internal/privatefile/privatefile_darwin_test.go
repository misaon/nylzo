package privatefile

import (
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

func TestReadbackRefusesAnInheritedACL(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	shareByACL(t, directory)
	file, err := os.Open(filepath.Join(directory, "usual"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if privateEntry(file) {
		t.Fatal("the readback accepted a 0600 file whose inherited ACE lets everyone read it")
	}
}
