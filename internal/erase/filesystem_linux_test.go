package erase

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
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
	if _, ok = parseMountInfo("36 35 259:2 /mnt1 /mnt2 rw,noatime master:1 shared:2 ext4 /dev/root", "259:2"); ok {
		t.Fatal("a line without the separator matched")
	}
	tmpfs := "41 30 0:45 / /run/nylzo-epoch rw,nosuid,nodev - tmpfs tmpfs rw,size=1024k,mode=700,uid=65532,noswap"
	found, ok = parseMountInfo(tmpfs, "0:45")
	if !ok || !hasOption(strings.Split(found.superOptions, ","), "noswap") {
		t.Fatalf("the noswap option of a tmpfs was not found: %+v %v", found, ok)
	}
}

func TestATmpfsThatSwapCanReachIsRefused(t *testing.T) {
	t.Parallel()
	active, swapErr := thisKernel.swapActive()
	onTmpfs, tmpfsErr := OnTmpfs("/dev/shm")
	if swapErr != nil || tmpfsErr != nil || !active || !onTmpfs {
		t.Skip("needs active swap and /dev/shm on a tmpfs")
	}
	directory, err := os.MkdirTemp("/dev/shm", "nylzo-erase-") //nolint:usetesting // must lie on /dev/shm
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err = RequireInPlace(directory); !errors.Is(err, ErrNotInPlace) {
		t.Fatalf("a tmpfs without noswap was accepted while swap is active: %v", err)
	}
}

// TestRequireInPlaceOnRealExt4 runs on loop images; it needs root, losetup, mkfs.ext4,
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

func TestRequireInPlaceSkipsAVanishedPathAndRefusesOneItCannotRead(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := RequireInPlace(filepath.Join(directory, "vanished")); err != nil {
		t.Fatalf("a path that vanished: %v", err)
	}
	file := filepath.Join(directory, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RequireInPlace(filepath.Join(file, "child")); !errors.Is(err, unix.ENOTDIR) {
		t.Fatalf("a path below a file: %v", err)
	}
	if err := RequireInPlace("/proc"); !errors.Is(err, ErrNotInPlace) || !strings.Contains(err.Error(), "type 0x") {
		t.Fatalf("an unknown file system: %v", err)
	}
	if _, err := OnTmpfs(filepath.Join(directory, "vanished")); !errors.Is(err, unix.ENOENT) {
		t.Fatalf("OnTmpfs of a missing path: %v", err)
	}
	if _, err := thisKernel.describeMount(filepath.Join(directory, "vanished"), 0); !errors.Is(err, unix.ENOENT) {
		t.Fatalf("describeMount of a missing path: %v", err)
	}
}

func TestTheInodeFlagsOfAnExt4PathDecide(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := journalsData(filepath.Join(directory, "vanished")); !errors.Is(err, unix.ENOENT) {
		t.Fatalf("the flags of a missing path: %v", err)
	}
	if _, err := journalsData(fifo); err == nil {
		t.Fatal("a FIFO has no inode flags to read")
	}
	var stat unix.Stat_t
	if err := unix.Stat(directory, &stat); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		flags func(string) (bool, error)
		check func(error) bool
	}{
		{func(string) (bool, error) { return false, unix.ENOENT }, func(err error) bool { return err == nil }},
		{func(string) (bool, error) { return false, unix.EIO }, func(err error) bool { return errors.Is(err, unix.EIO) }},
		{func(string) (bool, error) { return true, nil }, func(err error) bool { return errors.Is(err, ErrNotInPlace) }},
		{func(string) (bool, error) { return false, nil }, func(err error) bool { return err == nil }},
	} {
		host := kernel{journalsData: testCase.flags}
		checked := map[uint64]mount{stat.Dev: {fileSystem: ext4Name}}
		if err := host.requirePathInPlace(directory, checked); !testCase.check(err) {
			t.Errorf("an ext4 path: %v", err)
		}
	}
}

func TestTheKernelFilesDecideHowAMountIsDescribed(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	missing := filepath.Join(directory, "missing")
	device := unix.Mkdev(8, 1)
	noSwap := write("no-swap", "Filename Type Size Used Priority\n")
	swap := write("swap", "Filename Type Size Used Priority\n/swapfile file 1024 0 -2\n")
	mounts := write("mountinfo", "1 0 0:45 / /run rw - tmpfs tmpfs rw,noswap\n2 0 8:1 / /data rw - ext4 /dev/sda1 rw\n")
	unrelated := write("unrelated", "1 0 0:45 / /run rw - tmpfs tmpfs rw\n")
	if err := os.MkdirAll(filepath.Join(directory, "ext4", "sda1"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join("ext4", "sda1", "options"), "rw\ndata=journal\n")

	if _, err := (&kernel{swaps: missing}).describeTmpfs(device); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unreadable swaps: %v", err)
	}
	if found, err := (&kernel{swaps: noSwap}).describeTmpfs(device); err != nil || found.swapActive {
		t.Fatalf("no swap: %+v %v", found, err)
	}
	if _, err := (&kernel{swaps: swap, mountInfo: missing}).describeTmpfs(device); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unreadable mounts: %v", err)
	}
	if _, err := (&kernel{swaps: swap, mountInfo: unrelated}).describeTmpfs(device); !errors.Is(err, errNoMount) {
		t.Fatalf("no mount of the device: %v", err)
	}
	if found, err := (&kernel{swaps: swap, mountInfo: mounts}).describeTmpfs(unix.Mkdev(0, 45)); err != nil ||
		!found.swapActive || !found.noSwap {
		t.Fatalf("a tmpfs with noswap: %+v %v", found, err)
	}
	if _, err := (&kernel{mountInfo: missing}).ext4Options(device); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ext4 without mounts: %v", err)
	}
	if _, err := (&kernel{mountInfo: mounts, ext4: missing, blockDevices: missing}).ext4Options(device); !errors.Is(
		err,
		errNoProc,
	) {
		t.Fatalf("ext4 without effective options: %v", err)
	}
	host := kernel{mountInfo: mounts, ext4: filepath.Join(directory, "ext4"), blockDevices: missing}
	if options, err := host.ext4Options(device); err != nil || !hasOption(options, "data=journal") {
		t.Fatalf("the effective options name the data mode: %v %v", options, err)
	}
	host.blockDevices = filepath.Join(directory, "block")
	if err := os.Mkdir(host.blockDevices, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../devices/nvme0n1p1", filepath.Join(host.blockDevices, "8:1")); err != nil {
		t.Fatal(err)
	}
	if name := host.deviceName(device, "/dev/root"); name != "nvme0n1p1" {
		t.Fatalf("the kernel names the device behind /sys/dev/block, not the mount source: %s", name)
	}
}

func TestADescriptionTheKernelCannotGiveRefusesThePath(t *testing.T) {
	t.Parallel()
	if onTmpfs, err := OnTmpfs("/dev/shm"); err != nil || !onTmpfs {
		t.Skip("needs /dev/shm on a tmpfs")
	}
	host := kernel{swaps: filepath.Join(t.TempDir(), "missing")}
	if err := host.requirePathInPlace("/dev/shm", map[uint64]mount{}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a tmpfs whose swap state is unknown: %v", err)
	}
}
