//go:build !windows

package privatefile

import (
	"cmp"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const (
	fileMode      = 0o600
	directoryMode = 0o700
)

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

// MkdirTemp returns the new directory open, so files land in it even if its name is later replaced. It opens the
// name inside the parent, where a link that leads out of the parent is refused.
func MkdirTemp(directory, prefix string) (*os.Root, error) {
	directory = cmp.Or(directory, os.TempDir())
	base, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	name := prefix + rand.Text()
	if mkdirErr := mkdirIn(base, directory, name); mkdirErr != nil {
		return nil, mkdirErr
	}
	root, err := base.OpenRoot(name)
	if err != nil {
		return nil, errors.Join(err, base.Remove(name))
	}
	private, err := emptyPrivateDirectory(root)
	if private {
		return root, nil
	}
	cleanup := errors.Join(root.Close(), base.Remove(name))
	if err != nil {
		return nil, errors.Join(err, cleanup)
	}
	return nil, refuse(filepath.Join(directory, name), cleanup)
}

// emptyPrivateDirectory also requires no entries, since the directory MkdirTemp made is empty.
func emptyPrivateDirectory(root *os.Root) (bool, error) {
	opened, err := root.Open(".")
	if err != nil {
		return false, err
	}
	defer opened.Close()
	entries, err := opened.ReadDir(1)
	if len(entries) != 0 {
		return false, nil
	}
	if !errors.Is(err, io.EOF) {
		return false, err
	}
	return privateEntry(opened), nil
}

// privateEntry reads back what the file system kept, since FAT, exFAT, network and FUSE mounts report a mode they do
// not enforce.
func privateEntry(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid() && info.Mode().Perm()&0o077 == 0 &&
		enforcedLocally(file) && withoutACL(file)
}

func refuse(path string, cleanup error) error {
	return &fs.PathError{Op: "open", Path: path, Err: errors.Join(ErrNotPrivate, cleanup)}
}
