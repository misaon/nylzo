package erase

import "testing"

func TestMountInfoNamesTheFileSystemOfTheDevice(t *testing.T) {
	t.Parallel()
	line := "36 35 259:2 /mnt1 /mnt2 rw,noatime master:1 - ext4 /dev/root rw,errors=continue,data=journal"
	if found, ok := parseMountInfo(line, majorMinor(0x10302)); !ok || checkInPlace("/data", found) == nil {
		t.Fatalf("%+v %v", found, ok)
	}
	if _, ok := parseMountInfo(line, "8:1"); ok {
		t.Fatal("a mount of another device matched")
	}
}
