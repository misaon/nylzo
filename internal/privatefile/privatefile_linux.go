package privatefile

import (
	"os"
	"slices"

	"golang.org/x/sys/unix"
)

const virtiofsMagic = 0x6a656a63

// remoteFileSystems decide access on a server or in a helper process, whatever owner and mode they report here.
var remoteFileSystems = []uint32{
	unix.NFS_SUPER_MAGIC,
	unix.CIFS_SUPER_MAGIC,
	unix.SMB2_SUPER_MAGIC,
	unix.SMB_SUPER_MAGIC,
	unix.V9FS_MAGIC,
	unix.FUSE_SUPER_MAGIC,
	unix.CEPH_SUPER_MAGIC,
	unix.AFS_SUPER_MAGIC,
	unix.AFS_FS_MAGIC,
	unix.CODA_SUPER_MAGIC,
	unix.NCP_SUPER_MAGIC,
	virtiofsMagic,
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
	return !slices.Contains(remoteFileSystems, uint32(stat.Type)) //nolint:gosec // G115: f_type is a 32-bit magic
}

// withoutACL holds on Linux, where the group bits of a 0600 or 0700 mode set the ACL mask to no access.
func withoutACL(*os.File) bool {
	return true
}
