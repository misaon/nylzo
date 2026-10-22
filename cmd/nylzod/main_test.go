package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSweepOnceNeedsAnExistingStore(t *testing.T) {
	dir := t.TempDir()
	if err := existingStore(dir); err == nil {
		t.Fatal("sweep-once accepted an empty data directory")
	}
	if err := os.WriteFile(filepath.Join(dir, "nylzo.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := existingStore(dir); err == nil {
		t.Fatal("sweep-once accepted a store without a blob directory")
	}
	if err := os.Mkdir(filepath.Join(dir, "blobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := existingStore(dir); err != nil {
		t.Fatalf("sweep-once refused an existing store: %v", err)
	}
}
