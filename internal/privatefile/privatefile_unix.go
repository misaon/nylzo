//go:build !windows

package privatefile

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

const fileMode = 0o600

func Create(path string) (*os.File, error) {
	file, err := createNew(path)
	if err != nil {
		return nil, err
	}
	if !privateEntry(file) {
		return nil, refuse(path, errors.Join(file.Close(), os.Remove(path)))
	}
	return file, nil
}

func CreateIn(root *os.Root, name string) (*os.File, error) {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return nil, err
	}
	if !privateEntry(file) {
		return nil, refuse(file.Name(), errors.Join(file.Close(), root.Remove(name)))
	}
	return file, nil
}

// MkdirTemp returns the new directory open, so files land in it even if its name is later replaced.
func MkdirTemp(directory, prefix string) (*os.Root, error) {
	path, err := mkdirNew(directory, prefix)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.Join(err, os.Remove(path))
	}
	if opened, openErr := root.Open("."); openErr == nil {
		private := privateEntry(opened)
		opened.Close()
		if private {
			return root, nil
		}
	}
	return nil, refuse(path, errors.Join(root.Close(), os.Remove(path)))
}

// privateEntry reads back what the file system kept: FAT, exFAT and CIFS mounts ignore the mode.
func privateEntry(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid() && info.Mode().Perm()&0o077 == 0 && withoutACL(file)
}

func refuse(path string, cleanup error) error {
	return &fs.PathError{Op: "open", Path: path, Err: errors.Join(ErrNotPrivate, cleanup)}
}
