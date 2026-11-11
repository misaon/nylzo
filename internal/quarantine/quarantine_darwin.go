package quarantine

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const attribute = "com.apple.quarantine"

// Mark keeps a mark the system set itself, as it does for every file of a quarantined process. Otherwise it writes what
// macOS's own qtn_file_apply_to_path writes for QTN_FLAG_DOWNLOAD, with no agent or event that could hold an address.
// It reads first because x/sys drops the flags of Fsetxattr on darwin, so XATTR_CREATE cannot refuse an existing mark.
func Mark(file *os.File) error {
	descriptor := int(file.Fd())
	_, err := unix.Fgetxattr(descriptor, attribute, nil)
	if errors.Is(err, unix.ENOATTR) {
		return unix.Fsetxattr(descriptor, attribute, fmt.Appendf(nil, "0081;%08x;;", time.Now().Unix()), 0)
	}
	return err
}
