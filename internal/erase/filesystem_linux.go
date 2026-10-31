package erase

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	extendedMagic     = 0xEF53
	journalDataFlag   = 0x00004000
	getFlagsRequest   = 0x80086601
	mountInfoPath     = "/proc/self/mountinfo"
	mountInfoFields   = 10
	majorMinorField   = 2
	mountSeparator    = "-"
	minorBits         = 8
	extendedMinorBits = 12
	majorHighShift    = 32
)

var fileSystemNames = map[int64]string{
	0x58465342: "xfs",
	0x01021994: "tmpfs",
	0x9123683E: "btrfs",
	0x2FC12FC1: "zfs",
	0xCA451A4E: "bcachefs",
	0xF2F52010: "f2fs",
	0x3434:     "nilfs2",
	0x794C7630: "overlay",
	0x6969:     "nfs",
	0x65735546: "fuse",
	0xFF534D42: "cifs",
	0xFE534D42: "smb2",
	0x6A656A63: "virtiofs",
}

// RequireInPlace refuses a directory whose file system may keep old data when a file is overwritten.
func RequireInPlace(directory string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(directory, &stat); err != nil {
		return err
	}
	if stat.Type != extendedMagic {
		name, known := fileSystemNames[stat.Type]
		if !known {
			name = "file system type 0x" + strconv.FormatInt(stat.Type, 16)
		}
		return checkInPlace(directory, mount{fileSystem: name})
	}
	found, err := mountOf(directory)
	if err != nil {
		return fmt.Errorf("erase: read the mount options of %s: %w", directory, err)
	}
	if found.journaledDirectory, err = journalsData(directory); err != nil {
		return fmt.Errorf("erase: read the attributes of %s: %w", directory, err)
	}
	return checkInPlace(directory, found)
}

func mountOf(directory string) (mount, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(directory, &stat); err != nil {
		return mount{}, err
	}
	device := majorMinor(stat.Dev)
	file, err := os.Open(mountInfoPath)
	if err != nil {
		return mount{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if found, ok := parseMountInfo(scanner.Text(), device); ok {
			return found, nil
		}
	}
	return mount{}, errors.Join(scanner.Err(), errors.New("no mount matches the device"))
}

// parseMountInfo reads one line of proc_pid_mountinfo(5); the fields after "-" are type, source and super options.
func parseMountInfo(line, device string) (mount, bool) {
	fields := strings.Fields(line)
	if len(fields) < mountInfoFields || fields[majorMinorField] != device {
		return mount{}, false
	}
	for i, field := range fields {
		if field == mountSeparator && i+3 < len(fields) {
			return mount{fileSystem: fields[i+1], superOptions: fields[i+3]}, true
		}
	}
	return mount{}, false
}

// majorMinor decodes the device number as glibc's gnu_dev_major and gnu_dev_minor do.
func majorMinor(device uint64) string {
	major := (device>>minorBits)&0xfff | (device>>majorHighShift)&^0xfff
	minor := device&0xff | (device>>extendedMinorBits)&^0xff
	return strconv.FormatUint(major, 10) + ":" + strconv.FormatUint(minor&0xffffffff, 10)
}

// journalsData reads the inode flags, since new files inherit FS_JOURNAL_DATA_FL from their directory on ext4.
func journalsData(directory string) (bool, error) {
	handle, err := os.Open(directory)
	if err != nil {
		return false, err
	}
	defer handle.Close()
	var flags int64
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		handle.Fd(),
		getFlagsRequest,
		uintptr(unsafe.Pointer(&flags)),
	)
	if errno != 0 {
		return false, errno
	}
	return flags&journalDataFlag != 0, nil
}
