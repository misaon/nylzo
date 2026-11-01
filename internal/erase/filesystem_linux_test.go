package erase

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMountInfoNamesTheSourceAndOptionsOfTheDevice(t *testing.T) {
	t.Parallel()
	line := "36 35 259:2 /mnt1 /mnt2 rw,noatime master:1 - ext4 /dev/root rw,errors=continue,data=journal"
	found, ok := parseMountInfo(line, majorMinor(0x10302))
	if !ok || found.source != "/dev/root" || !hasOption(strings.Split(found.superOptions, ","), "data=journal") {
		t.Fatalf("%+v %v", found, ok)
	}
	if _, ok = parseMountInfo(line, "8:1"); ok {
		t.Fatal("a mount of another device matched")
	}
}

// TestRequireInPlaceOnRealExt4 covers the final review's M2 on loop images; it needs root, losetup, mkfs.ext4,
// tune2fs and chattr, so it runs in the privileged lab container with NYLZO_TEST_EXT4_LOOP=1.
func TestRequireInPlaceOnRealExt4(t *testing.T) {
	if os.Getenv("NYLZO_TEST_EXT4_LOOP") != "1" || os.Geteuid() != 0 {
		t.Skip("set NYLZO_TEST_EXT4_LOOP=1 and run as root with e2fsprogs to mount ext4 loop images")
	}
	mountImage := func(t *testing.T, tune []string, mountOptions string) string {
		t.Helper()
		directory := t.TempDir()
		image, target := filepath.Join(directory, "disk.img"), filepath.Join(directory, "mount")
		run(t, "truncate", "-s", "64M", image)
		run(t, "mkfs.ext4", "-q", "-F", image)
		if len(tune) > 0 {
			run(t, "tune2fs", append(tune, image)...)
		}
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		arguments := []string{"-o", "loop"}
		if mountOptions != "" {
			arguments = []string{"-o", "loop," + mountOptions}
		}
		run(t, "mount", append(arguments, image, target)...)
		t.Cleanup(func() { exec.Command("umount", target).Run() })
		return target
	}
	plain := mountImage(t, nil, "")
	if err := RequireInPlace(plain); err != nil {
		t.Fatalf("plain ext4 in data=ordered must pass: %v", err)
	}
	file := filepath.Join(plain, "nylzo.db")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "chattr", "+j", file)
	journaledDirectory := filepath.Join(plain, "blobs")
	if err := os.Mkdir(journaledDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	run(t, "chattr", "+j", journaledDirectory)
	for name, path := range map[string]string{
		"data=journal in the mount options":  mountImage(t, nil, "data=journal"),
		"journal_data as superblock default": mountImage(t, []string{"-o", "journal_data"}, ""),
		"a file with chattr +j":              file,
		"a directory with chattr +j":         journaledDirectory,
	} {
		if err := RequireInPlace(plain, path); !errors.Is(err, ErrNotInPlace) {
			t.Errorf("%s must be refused: %v", name, err)
		}
	}
}

func run(t *testing.T, name string, arguments ...string) {
	t.Helper()
	if output, err := exec.Command(name, arguments...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v %s", name, arguments, err, output)
	}
}
