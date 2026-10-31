package erase

import (
	"errors"
	"testing"
)

func TestOnlyFileSystemsThatOverwriteInPlaceAreAccepted(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		found    mount
		accepted bool
	}{
		{mount{fileSystem: "ext4", superOptions: "rw,errors=remount-ro"}, true},
		{mount{fileSystem: "ext4", superOptions: "rw,data=writeback"}, true},
		{mount{fileSystem: "xfs", superOptions: "rw,attr2,inode64"}, true},
		{mount{fileSystem: "tmpfs", superOptions: "rw,size=65536k"}, true},
		{mount{fileSystem: "ext4", superOptions: "rw,data=journal"}, false},
		{mount{fileSystem: "ext4", superOptions: "rw", journaledDirectory: true}, false},
		{mount{fileSystem: "btrfs", superOptions: "rw,ssd"}, false},
		{mount{fileSystem: "zfs", superOptions: "rw,xattr"}, false},
		{mount{fileSystem: "overlay", superOptions: "rw,lowerdir=/a"}, false},
		{mount{fileSystem: "a darwin file system"}, false},
	} {
		err := checkInPlace("/data", testCase.found)
		if accepted := err == nil; accepted != testCase.accepted || (err != nil && !errors.Is(err, ErrNotInPlace)) {
			t.Errorf("%+v: %v", testCase.found, err)
		}
	}
}
