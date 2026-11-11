package privatefile

import (
	"errors"
	"os"
	"slices"

	"golang.org/x/sys/unix"
)

const (
	zfsMagic   = 0x2fc12fc1
	nfs4ACLKey = "system.nfs4_acl"
)

// localFileSystems keep owner and mode on this machine and enforce them there; any other type is refused, since a
// network, FUSE, shared-folder or FAT mount reports a mode that a server, helper or mount option decides.
var localFileSystems = []uint32{
	unix.EXT4_SUPER_MAGIC,
	unix.XFS_SUPER_MAGIC,
	unix.BTRFS_SUPER_MAGIC,
	unix.TMPFS_MAGIC,
	unix.RAMFS_MAGIC,
	unix.OVERLAYFS_SUPER_MAGIC,
	unix.ECRYPTFS_SUPER_MAGIC,
	unix.F2FS_SUPER_MAGIC,
	unix.BCACHEFS_SUPER_MAGIC,
	unix.NILFS_SUPER_MAGIC,
	unix.REISERFS_SUPER_MAGIC,
	zfsMagic,
}

func createNew(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
}

func mkdirIn(base *os.Root, _, name string) error {
	return base.Mkdir(name, directoryMode)
}

func enforcedLocally(file *os.File) bool {
	var stat unix.Statfs_t
	if unix.Fstatfs(int(file.Fd()), &stat) != nil {
		return false
	}
	return slices.Contains(localFileSystems, uint32(stat.Type)) //nolint:gosec // G115: f_type is a 32-bit magic
}

// withoutACL refuses an NFSv4 ACL, as ZFS keeps with acltype=nfsv4, whose entries the mode does not show. A POSIX ACL
// needs no check: the group bits of a 0600 or 0700 mode set its mask to no access.
func withoutACL(file *os.File) bool {
	_, err := unix.Fgetxattr(int(file.Fd()), nfs4ACLKey, nil)
	return errors.Is(err, unix.ENODATA) || errors.Is(err, unix.EOPNOTSUPP)
}
