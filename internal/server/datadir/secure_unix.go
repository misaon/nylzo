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
	info, err := os.Stat(layout.Directory)
	if err != nil {
		return 0, err
	}
	narrowed := 0
	if err = secureEntry(layout.Directory, info, &narrowed); err != nil {
		return narrowed, fmt.Errorf("%w (the data directory)", err)
	}
	for _, path := range layout.Files {
		if info, err = os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return narrowed, err
		}
		if err = secureEntry(path, info, &narrowed); err != nil {
			return narrowed, fmt.Errorf("%w (%s)", err, filepath.Base(path))
		}
	}
	for _, tree := range layout.Trees {
		if err = secureTree(tree, &narrowed); err != nil {
			return narrowed, fmt.Errorf("%w (in %s)", err, filepath.Base(tree))
		}
	}
	return narrowed, nil
}

func secureTree(tree string, narrowed *int) error {
	return filepath.WalkDir(tree, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, fs.ErrNotExist) && path == tree {
			return fs.SkipDir
		}
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return secureEntry(path, info, narrowed)
	})
}

func secureEntry(path string, info fs.FileInfo, narrowed *int) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		return ErrSymlink
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return ErrForeignOwner
	}
	if info.Mode().Perm()&0o077 == 0 {
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
