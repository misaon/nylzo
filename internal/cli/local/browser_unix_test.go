//go:build unix

package local

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
)

// A browser that the opener starts lives on unprotected, so it must not inherit NYLZO_LINK with the key of a link.
func TestTheOpenerGetsNoNylzoVariable(t *testing.T) {
	directory := t.TempDir()
	opener := filepath.Join(directory, systemOpener("")[0])
	script := "#!/bin/sh\n/usr/bin/env > \"$1.part\" && /bin/mv \"$1.part\" \"$1\"\n"
	if err := os.WriteFile(opener, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("NYLZO_LINK", "https://nylzo.example/r#2.secret")
	t.Setenv("NYLZO_PASSPHRASE_FILE", "/private/passphrase")
	report := filepath.Join(directory, "environment")
	if err := browser(&command.Environment{})(report); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	environment, err := os.ReadFile(report)
	for err != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		environment, err = os.ReadFile(report)
	}
	if err != nil {
		t.Fatalf("the opener wrote no environment: %v", err)
	}
	if !strings.Contains(string(environment), "PATH=") || strings.Contains(string(environment), "NYLZO_") {
		t.Fatalf("the opener got %q", environment)
	}
}

func TestNoBrowserOpensWithoutTheSystemOpener(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := openWithSystem(filepath.Join(t.TempDir(), "open.html")); err == nil {
		t.Fatal("opened a browser without the system opener")
	}
}

func TestAHandoffPageThatCannotBeWrittenLeavesNothingBehind(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	refused := errors.New("refused")
	page, err := writeHandoffFile("http://session.localhost/open#token", func(*os.Root, string) (*os.File, error) {
		return nil, refused
	})
	if !errors.Is(err, refused) || page.path != "" {
		t.Fatalf("an unwritable page: %+v, %v", page, err)
	}
	if left, _ := os.ReadDir(temporary); len(left) != 0 {
		t.Fatalf("the refused page left %v", left)
	}
	t.Setenv("TMPDIR", filepath.Join(temporary, "absent"))
	if _, err = writeHandoffFile("http://session.localhost/open#token", nil); err == nil {
		t.Fatal("wrote a page without a temporary directory")
	}
}
