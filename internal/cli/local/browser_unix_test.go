//go:build unix

package local

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheOpenerGetsNoNylzoVariable covers the F07 review: a browser that the opener starts lives on unprotected, so
// it must not inherit NYLZO_LINK with the key of a link.
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
	if err := openWithSystem(report); err != nil {
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
