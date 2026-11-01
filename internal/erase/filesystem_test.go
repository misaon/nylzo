package erase

import (
	"errors"
	"strings"
	"testing"
)

func TestOnlyFileSystemsThatOverwriteInPlaceAreAccepted(t *testing.T) {
	t.Parallel()
	options := func(text string) []string { return strings.Split(text, ",") }
	for _, testCase := range []struct {
		found    mount
		accepted bool
	}{
		{mount{fileSystem: "ext4", dataOptions: options("rw,errors=remount-ro,data=ordered")}, true},
		{mount{fileSystem: "ext4", dataOptions: options("rw,data=writeback")}, true},
		{mount{fileSystem: "xfs"}, true},
		{mount{fileSystem: "tmpfs"}, true},
		{mount{fileSystem: "tmpfs", swapActive: true}, false},
		{mount{fileSystem: "ext4", dataOptions: options("rw,data=journal")}, false},
		{mount{fileSystem: "ext4", dataOptions: options("rw,data=ordered"), journaledData: true}, false},
		{mount{fileSystem: "btrfs"}, false},
		{mount{fileSystem: "zfs"}, false},
		{mount{fileSystem: "overlay"}, false},
		{mount{fileSystem: "a darwin file system"}, false},
	} {
		err := checkInPlace("/data", testCase.found)
		if accepted := err == nil; accepted != testCase.accepted || (err != nil && !errors.Is(err, ErrNotInPlace)) {
			t.Errorf("%+v: %v", testCase.found, err)
		}
	}
}
