//go:build darwin || linux || windows

package privatefile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEntriesStayPrivateInADirectoryEveryoneCanRead(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	shareWithEveryone(t, parent)

	passphrase := filepath.Join(parent, "passphrase")
	file, err := Create(passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("secret\n"); err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	requirePrivate(t, passphrase, false)

	root, err := MkdirTemp(parent, "nylzo-")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	directory := root.Name()
	if filepath.Dir(directory) != parent || !strings.HasPrefix(filepath.Base(directory), "nylzo-") {
		t.Fatalf("MkdirTemp made %s", directory)
	}
	requirePrivate(t, directory, true)
	if file, err = CreateIn(root, "message.txt"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	requirePrivate(t, filepath.Join(directory, "message.txt"), false)
}

func TestCreateNeverReplacesAnEntryOrFollowsALink(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	existing := filepath.Join(directory, "existing")
	if err := os.WriteFile(existing, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "target")
	if err := os.Symlink(target, filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"existing", "link"} {
		if file, createErr := Create(filepath.Join(directory, name)); !errors.Is(createErr, fs.ErrExist) {
			file.Close()
			t.Errorf("Create(%s): %v", name, createErr)
		}
		if file, createErr := CreateIn(root, name); !errors.Is(createErr, fs.ErrExist) {
			file.Close()
			t.Errorf("CreateIn(%s): %v", name, createErr)
		}
	}
	if content, readErr := os.ReadFile(existing); readErr != nil || string(content) != "kept" {
		t.Fatalf("the existing file changed: %q %v", content, readErr)
	}
	if _, err = os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the link target was created: %v", err)
	}
}
