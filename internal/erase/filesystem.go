package erase

import (
	"errors"
	"fmt"
	"strings"
)

var ErrNotInPlace = errors.New("erase: the file system may keep the old data when a file is overwritten")

type FileSystemError struct {
	Directory  string
	FileSystem string
	Reason     string
}

func (e *FileSystemError) Error() string {
	return fmt.Sprintf(
		"%s is on %s, which %s, so a deleted message could be read again from the disk; "+
			"use ext4 (data=ordered or data=writeback), XFS or tmpfs on Linux",
		e.Directory,
		e.FileSystem,
		e.Reason,
	)
}

func (*FileSystemError) Unwrap() error {
	return ErrNotInPlace
}

type mount struct {
	fileSystem         string
	superOptions       string
	journaledDirectory bool
}

// checkInPlace accepts only file systems that write an overwrite to the blocks the file already has.
func checkInPlace(directory string, found mount) error {
	refuse := func(reason string) error {
		return &FileSystemError{Directory: directory, FileSystem: found.fileSystem, Reason: reason}
	}
	switch found.fileSystem {
	case "ext2", "ext3", "ext4":
		if hasOption(found.superOptions, "data=journal") {
			return refuse("writes file data to its journal first (data=journal)")
		}
		if found.journaledDirectory {
			return refuse("journals the data of new files in this directory (chattr +j)")
		}
		return nil
	case "xfs", "tmpfs":
		return nil
	case "btrfs", "zfs", "bcachefs", "f2fs", "nilfs2":
		return refuse("writes changed data to new blocks (copy-on-write or log-structured)")
	case "overlay":
		return refuse("copies files up to another layer instead of overwriting them")
	default:
		return refuse("is not known to overwrite data in place")
	}
}

func hasOption(options, wanted string) bool {
	for option := range strings.SplitSeq(options, ",") {
		if option == wanted {
			return true
		}
	}
	return false
}
