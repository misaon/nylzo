package erase

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	ext4Name  = "ext4"
	tmpfsName = "tmpfs"
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
			"use ext4 (data=ordered or data=writeback), XFS, or tmpfs without swap on Linux",
		e.Directory,
		e.FileSystem,
		e.Reason,
	)
}

func (*FileSystemError) Unwrap() error {
	return ErrNotInPlace
}

// mount describes where a path lies; dataOptions holds the effective ext4 options, every data= mode included.
type mount struct {
	fileSystem    string
	dataOptions   []string
	journaledData bool
	swapActive    bool
}

// checkInPlace accepts only file systems that write an overwrite to the blocks the file already has.
func checkInPlace(path string, found mount) error {
	refuse := func(reason string) error {
		return &FileSystemError{Directory: path, FileSystem: found.fileSystem, Reason: reason}
	}
	switch found.fileSystem {
	case ext4Name:
		if hasOption(found.dataOptions, "data=journal") {
			return refuse("writes file data to its journal first (data=journal, also as a superblock default)")
		}
		if found.journaledData {
			return refuse("journals the data of this file, or of new files in this directory (chattr +j)")
		}
		return nil
	case tmpfsName:
		if found.swapActive {
			return refuse("can write its pages to swap, which keeps them after an overwrite")
		}
		return nil
	case "xfs":
		return nil
	case "btrfs", "zfs", "bcachefs", "f2fs", "nilfs2":
		return refuse("writes changed data to new blocks (copy-on-write or log-structured)")
	case "overlay":
		return refuse("copies files up to another layer instead of overwriting them")
	default:
		return refuse("is not known to overwrite data in place")
	}
}

func hasOption(options []string, wanted string) bool {
	return slices.ContainsFunc(options, func(option string) bool { return strings.TrimSpace(option) == wanted })
}
