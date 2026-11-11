package privatefile

import (
	"cmp"
	"crypto/rand"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"
)

// filesec is xnu's struct kauth_filesec without entries; its owner and group GUIDs stay unset.
type filesec struct {
	magic      uint32
	owner      [16]byte
	group      [16]byte
	entryCount uint32
	flags      uint32
}

const (
	directoryMode = 0o700
	filesecMagic  = 0x012cc16d
	// aclNoInherit is KAUTH_ACL_NO_INHERIT: the new entry takes no ACE from its parent directory.
	aclNoInherit = 1 << 17
	unsetID      = ^uint32(0) - 100
	filesecSize  = int(unsafe.Sizeof(filesec{}))
)

// macOS checks ACL entries before the mode, so an inheritable ACE of the parent would open a 0600 file to others.
var noInheritance = filesec{magic: filesecMagic, flags: aclNoInherit}

func createNew(path string) (*os.File, error) {
	name, err := unix.BytePtrFromString(path)
	if err != nil {
		return nil, err
	}
	descriptor, _, errno := unix.Syscall6(
		unix.SYS_OPEN_EXTENDED, //nolint:staticcheck // SA1019: libSystem's openx_np, which makes this call, has no Go wrapper
		uintptr(unsafe.Pointer(name)),
		uintptr(unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC),
		uintptr(unsetID),
		uintptr(unsetID),
		fileMode,
		uintptr(unsafe.Pointer(&noInheritance)),
	)
	if errno != 0 {
		return nil, &os.PathError{Op: "open", Path: path, Err: errno}
	}
	return os.NewFile(descriptor, path), nil
}

func mkdirNew(directory, prefix string) (string, error) {
	path := filepath.Join(cmp.Or(directory, os.TempDir()), prefix+rand.Text())
	name, err := unix.BytePtrFromString(path)
	if err != nil {
		return "", err
	}
	_, _, errno := unix.Syscall6(
		unix.SYS_MKDIR_EXTENDED, //nolint:staticcheck // SA1019: libSystem's mkdirx_np, which makes this call, has no Go wrapper
		uintptr(unsafe.Pointer(name)),
		uintptr(unsetID),
		uintptr(unsetID),
		directoryMode,
		uintptr(unsafe.Pointer(&noInheritance)),
		0,
	)
	if errno != 0 {
		return "", &os.PathError{Op: "mkdirtemp", Path: path, Err: errno}
	}
	return path, nil
}

// withoutACL asks fstat64_extended for the entry's filesec, whose size exceeds the bare header once it holds an ACE.
func withoutACL(file *os.File) bool {
	var stat unix.Stat_t
	var security filesec
	size := uint64(filesecSize)
	_, _, errno := unix.Syscall6(
		unix.SYS_FSTAT64_EXTENDED, //nolint:staticcheck // SA1019: libSystem's fstatx_np, which makes this call, has no Go wrapper
		file.Fd(),
		uintptr(unsafe.Pointer(&stat)),
		uintptr(unsafe.Pointer(&security)),
		uintptr(unsafe.Pointer(&size)),
		0,
		0,
	)
	return errno == 0 && size <= uint64(filesecSize)
}
