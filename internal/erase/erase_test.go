package erase_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/misaon/nylzo/internal/erase"
)

func TestShredOverwritesTheEraseBlockWhereItLies(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path, link := filepath.Join(directory, "blob"), filepath.Join(directory, "same-inode")
	content := make([]byte, 3*erase.BlockLen)
	rand.Read(content)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if err := erase.Shred(path, erase.BlockLen); err != nil {
		t.Fatal(err)
	}
	left, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left[:erase.BlockLen], make([]byte, erase.BlockLen)) {
		t.Fatal("the erase block still holds data in the file's own blocks")
	}
	if !bytes.Equal(left[erase.BlockLen:], content[erase.BlockLen:]) {
		t.Fatal("the overwrite reached past the erase block")
	}
	if _, err = os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the name must be gone: %v", err)
	}
	if err = erase.Remove(path, erase.BlockLen); err != nil {
		t.Fatalf("a missing file counts as removed: %v", err)
	}
}

func TestOverwriteNeverFollowsALink(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	target, symlink := filepath.Join(directory, "target"), filepath.Join(directory, "symlink")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	if err := erase.Overwrite(symlink, erase.WholeFile); err == nil {
		t.Fatal("a symbolic link must not be followed")
	}
	if kept, _ := os.ReadFile(target); string(kept) != "keep" {
		t.Fatal("the link target was overwritten")
	}
}

func TestReplaceWritesOverTheWholeFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "seed")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := erase.Replace(path, []byte("long second")); !errors.Is(err, erase.ErrLength) {
		t.Fatalf("content of another length must be refused: %v", err)
	}
	if err := erase.Replace(path, []byte("later")); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(path); string(content) != "later" {
		t.Fatalf("content %q", content)
	}
}
