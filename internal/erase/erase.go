// Package erase overwrites data where it lies before a file goes, so a disk image taken afterwards holds no copy.
package erase

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
)

const (
	// BlockLen is the size of the erase block at the start of a file: one file system block.
	BlockLen  = 4096
	WholeFile = math.MaxInt64

	zeroBufferLen = 64 << 10
)

var (
	ErrNotRegular = errors.New("erase: not a regular file")
	ErrLength     = errors.New("erase: the new content must have the length of the file it replaces")
)

// Overwrite replaces the first length bytes of the file with zeros where they lie and syncs them to the disk.
func Overwrite(path string, length int64) error {
	file, err := openForOverwrite(path)
	if err != nil {
		return err
	}
	size, err := regularSize(file)
	if err == nil {
		err = writeZeros(file, min(length, size))
	}
	return closeSynced(file, err)
}

// Replace writes content over the whole file where it lies, so the old content is gone once it returns.
func Replace(path string, content []byte) error {
	file, err := openForOverwrite(path)
	if err != nil {
		return err
	}
	size, err := regularSize(file)
	if err == nil && size != int64(len(content)) {
		err = ErrLength
	}
	if err == nil {
		_, err = file.WriteAt(content, 0)
	}
	return closeSynced(file, err)
}

// Shred overwrites the first length bytes and unlinks the file; the caller syncs the directory, once per batch.
func Shred(path string, length int64) error {
	err := Overwrite(path, length)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Remove shreds the file and syncs its directory, so neither the data nor the name comes back after a crash.
func Remove(path string, length int64) error {
	if err := Shred(path, length); err != nil {
		return err
	}
	return SyncDirectory(filepath.Dir(path))
}

func SyncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(handle.Sync(), handle.Close())
}

func regularSize(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: %s", ErrNotRegular, info.Mode().Type())
	}
	return info.Size(), nil
}

func writeZeros(file *os.File, length int64) error {
	zeros := make([]byte, min(length, zeroBufferLen))
	for offset := int64(0); offset < length; {
		chunk := zeros[:min(int64(len(zeros)), length-offset)]
		written, err := file.WriteAt(chunk, offset)
		if err != nil {
			return err
		}
		offset += int64(written)
	}
	return nil
}

func closeSynced(file *os.File, err error) error {
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}
