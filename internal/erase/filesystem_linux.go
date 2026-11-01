package erase

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	journalDataFlag = 0x00004000
	zfsMagic        = 0x2FC12FC1
	virtiofsMagic   = 0x6A656A63
	mountInfoPath   = "/proc/self/mountinfo"
	swapsPath       = "/proc/swaps"
	procExt4        = "/proc/fs/ext4"
	sysBlock        = "/sys/dev/block"
	mountInfoFields = 10
	majorMinorField = 2
	mountSeparator  = "-"
)

var (
	errNoProc  = errors.New("the kernel shows no effective ext4 options for this device")
	errNoMount = errors.New("no mount matches the device")
)

var fileSystemNames = map[int64]string{
	unix.XFS_SUPER_MAGIC:       "xfs",
	unix.TMPFS_MAGIC:           tmpfsName,
	unix.BTRFS_SUPER_MAGIC:     "btrfs",
	zfsMagic:                   "zfs",
	unix.BCACHEFS_SUPER_MAGIC:  "bcachefs",
	unix.F2FS_SUPER_MAGIC:      "f2fs",
	unix.NILFS_SUPER_MAGIC:     "nilfs2",
	unix.OVERLAYFS_SUPER_MAGIC: "overlay",
	unix.NFS_SUPER_MAGIC:       "nfs",
	unix.FUSE_SUPER_MAGIC:      "fuse",
	unix.CIFS_SUPER_MAGIC:      "cifs",
	unix.SMB2_SUPER_MAGIC:      "smb2",
	virtiofsMagic:              "virtiofs",
}

// RequireInPlace refuses any path whose file system, mount defaults or inode flags may keep old data when a file is
// overwritten. It reads the effective ext4 options from /proc/fs/ext4/<device>/options, which name the data mode even
// when it comes from the superblock defaults (tune2fs -o journal_data) and the mount table does not show it.
func RequireInPlace(paths ...string) error {
	checked := map[uint64]mount{}
	for _, path := range paths {
		var stat unix.Stat_t
		if err := unix.Stat(path, &stat); err != nil {
			return fmt.Errorf("erase: %s: %w", path, err)
		}
		found, seen := checked[stat.Dev]
		if !seen {
			var err error
			if found, err = describeMount(path, stat.Dev); err != nil {
				return fmt.Errorf("erase: %s: %w", path, err)
			}
			checked[stat.Dev] = found
		}
		if found.fileSystem == ext4Name {
			journaled, err := journalsData(path)
			if err != nil {
				return fmt.Errorf("erase: read the attributes of %s: %w", path, err)
			}
			found.journaledData = journaled
		}
		if err := checkInPlace(path, found); err != nil {
			return err
		}
	}
	return nil
}

func describeMount(path string, device uint64) (mount, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return mount{}, err
	}
	switch stat.Type {
	case unix.EXT4_SUPER_MAGIC:
		options, err := ext4Options(device)
		return mount{fileSystem: ext4Name, dataOptions: options}, err
	case unix.TMPFS_MAGIC:
		active, err := swapActive()
		return mount{fileSystem: tmpfsName, swapActive: active}, err
	default:
		name, known := fileSystemNames[stat.Type]
		if !known {
			name = "file system type 0x" + strconv.FormatInt(stat.Type, 16)
		}
		return mount{fileSystem: name}, nil
	}
}

// ext4Options joins the effective options with the super options of the mount table and refuses to guess without
// the effective ones, since only they show a data mode that the superblock sets.
func ext4Options(device uint64) ([]string, error) {
	found, err := mountOf(device)
	if err != nil {
		return nil, err
	}
	options := strings.Split(found.superOptions, ",")
	effective, err := os.ReadFile(filepath.Join(procExt4, kernelDeviceName(device, found.source), "options"))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoProc, err)
	}
	return append(options, strings.Split(string(effective), "\n")...), nil
}

// kernelDeviceName is the name the kernel gives /proc/fs/ext4/<name>: the block device behind /sys/dev/block, or the
// last element of the mount source when sysfs is not mounted.
func kernelDeviceName(device uint64, source string) string {
	if target, err := os.Readlink(filepath.Join(sysBlock, majorMinor(device))); err == nil {
		return filepath.Base(target)
	}
	return filepath.Base(source)
}

func swapActive() (bool, error) {
	swaps, err := os.ReadFile(swapsPath)
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimSpace(string(swaps)), "\n")
	return len(lines) > 1, nil
}

type mountLine struct {
	source       string
	superOptions string
}

func mountOf(device uint64) (mountLine, error) {
	file, err := os.Open(mountInfoPath)
	if err != nil {
		return mountLine{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if found, ok := parseMountInfo(scanner.Text(), majorMinor(device)); ok {
			return found, nil
		}
	}
	return mountLine{}, errors.Join(scanner.Err(), errNoMount)
}

// parseMountInfo reads one line of proc_pid_mountinfo(5); the fields after "-" are type, source and super options.
func parseMountInfo(line, device string) (mountLine, bool) {
	fields := strings.Fields(line)
	if len(fields) < mountInfoFields || fields[majorMinorField] != device {
		return mountLine{}, false
	}
	for i, field := range fields {
		if field == mountSeparator && i+3 < len(fields) {
			return mountLine{source: fields[i+2], superOptions: fields[i+3]}, true
		}
	}
	return mountLine{}, false
}

func majorMinor(device uint64) string {
	return strconv.FormatUint(uint64(unix.Major(device)), 10) + ":" + strconv.FormatUint(uint64(unix.Minor(device)), 10)
}

// journalsData reads the inode flags: a file with FS_JOURNAL_DATA_FL journals its data, and new files inherit the flag
// from their directory.
func journalsData(path string) (bool, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(descriptor)
	flags, err := unix.IoctlGetUint32(descriptor, unix.FS_IOC_GETFLAGS)
	if err != nil {
		return false, err
	}
	return flags&journalDataFlag != 0, nil
}
