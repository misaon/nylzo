//go:build !windows

package datadir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const (
	privateDirectoryMode fs.FileMode = 0o700
	privateFileMode      fs.FileMode = 0o600
)

// Secure narrows group and other access on entries the process owns and refuses entries owned by another user.
func Secure(layout Layout) (int, error) {
	narrowed := 0
	err := walkLayout(layout, &narrowed)
	return narrowed, err
}

// Check refuses what Secure refuses and changes nothing, so it can run on a directory mounted read-only.
func Check(layout Layout) error {
	return walkLayout(layout, nil)
}

func walkLayout(layout Layout, narrowed *int) error {
	info, err := os.Stat(layout.Directory)
	if err != nil {
		return err
	}
	if err = secureEntry(layout.Directory, info, narrowed); err != nil {
		return fmt.Errorf("%w (the data directory)", err)
	}
	for _, path := range layout.Files {
		if info, err = os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err = secureEntry(path, info, narrowed); err != nil {
			return fmt.Errorf("%w (%s)", err, filepath.Base(path))
		}
	}
	for _, tree := range layout.Trees {
		if err = secureTree(tree, narrowed); err != nil {
			return fmt.Errorf("%w (in %s)", err, filepath.Base(tree))
		}
	}
	return nil
}

// secureTree skips an entry that vanished while it walked, since uploads and sweeps of a running server rename and
// remove files in the tree.
func secureTree(tree string, narrowed *int) error {
	return filepath.WalkDir(tree, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, fs.ErrNotExist) {
			return skipVanished(path == tree, entry)
		}
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = secureEntry(path, info, narrowed); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
}

func skipVanished(root bool, entry fs.DirEntry) error {
	if root || (entry != nil && entry.IsDir()) {
		return fs.SkipDir
	}
	return nil
}

func secureEntry(path string, info fs.FileInfo, narrowed *int) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		return ErrSymlink
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return ErrForeignOwner
	}
	if info.Mode().Perm()&0o077 == 0 || narrowed == nil {
		return nil
	}
	mode := privateFileMode
	if info.IsDir() {
		mode = privateDirectoryMode
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	*narrowed++
	return nil
}
