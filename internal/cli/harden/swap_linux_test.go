package harden

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sysfs builds a fake /sys: a directory per block device under devices, with dm/uuid, slaves links to its lower
// devices and, for a partition, a place inside its disk; dev/block links each major:minor to its directory.
type sysfs struct {
	t    *testing.T
	root string
}

func (s sysfs) devices() string {
	return filepath.Join(s.root, "dev", "block")
}

func (s sysfs) add(number, path, uuid string, lower ...string) {
	s.t.Helper()
	directory := filepath.Join(s.root, "devices", path)
	files := map[string]string{}
	if uuid != "" {
		files[filepath.Join("dm", "uuid")] = uuid + "\n"
	}
	if strings.Contains(path, "/") {
		files["partition"] = "1\n"
	}
	for _, name := range []string{"slaves", "dm"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
			s.t.Fatal(err)
		}
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			s.t.Fatal(err)
		}
	}
	for _, name := range lower {
		target := filepath.Join(s.root, "devices", name)
		if err := os.Symlink(target, filepath.Join(directory, "slaves", filepath.Base(name))); err != nil {
			s.t.Fatal(err)
		}
	}
	if err := os.MkdirAll(s.devices(), 0o700); err != nil {
		s.t.Fatal(err)
	}
	if err := os.Symlink(directory, filepath.Join(s.devices(), number)); err != nil {
		s.t.Fatal(err)
	}
}

func (s sysfs) write(path, content string) {
	s.t.Helper()
	full := filepath.Join(s.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		s.t.Fatal(err)
	}
}

// escape writes a path the way the kernel lists it in /proc/swaps.
func escape(path string) string {
	var out strings.Builder
	for _, character := range []byte(path) {
		if strings.IndexByte(" \t\n\\", character) >= 0 {
			fmt.Fprintf(&out, "\\%03o", character)
			continue
		}
		out.WriteByte(character)
	}
	return out.String()
}

// TestClearSwapNamesOnlyAreasThatReachADiskInClear covers F07: dm-crypt must sit under every path of the stack, zram
// counts as memory only without a writeback device, and whatever nylzo cannot follow counts as clear.
func TestClearSwapNamesOnlyAreasThatReachADiskInClear(t *testing.T) {
	tree := sysfs{t: t, root: t.TempDir()}
	tree.add("259:0", "nvme0n1", "")
	tree.add("259:2", "nvme0n1/nvme0n1p2", "")
	tree.add("259:3", "nvme0n1/nvme0n1p3", "")
	tree.add("253:0", "dm-0", "CRYPT-LUKS2-5a1d0b6c8d2e4f3a9b7c6d5e4f3a2b1c-luks-root", "nvme0n1/nvme0n1p3")
	tree.add("253:1", "dm-1", "LVM-xP0b9Y4cQ1r", "dm-0")
	tree.add("253:2", "dm-2", "CRYPT-INTEGRITY-77aa-swap")
	tree.add("253:3", "dm-3", "LVM-mixed", "dm-0", "nvme0n1/nvme0n1p2")
	tree.add("253:4", "dm-4", "CRYPT-PLAIN-swap", "nvme0n1/nvme0n1p2")
	tree.add("253:5", "dm-5", "CRYPT-LUKS1-0b1c-disk")
	tree.add("253:6", "dm-5/dm-5p1", "")
	tree.add("252:0", "bcache0", "", "dm-0")
	tree.add("251:0", "zram0", "")
	tree.add("251:1", "zram1", "")
	tree.add("251:2", "zram2", "")
	tree.add("251:3", "zram3", "")
	tree.add("7:0", "loop0", "")
	tree.write("devices/zram1/backing_dev", "none\n")
	tree.write("devices/zram2/backing_dev", "/dev/nvme0n1p2\n")
	tree.write("devices/zram3/backing_dev", "/dev/dm-4\n")
	numbers := map[string]string{
		"/swap.img": "259:2", "/var/swap file": "253:1", "/srv/back\\slash": "253:1", "/dev/dm-2": "253:2",
		"/dev/dm-3": "253:3", "/dev/dm-4": "253:4", "/dev/dm-5p1": "253:6", "/dev/bcache0": "252:0",
		"/dev/zram0": "251:0", "/dev/zram1": "251:1", "/dev/zram2": "251:2", "/dev/zram3": "251:3",
		"/dev/loop0": "7:0", "/srv/btrfs/swap": "0:47", "/dev/nvme0n1p2": "259:2", "/dev/nvme0n1p3": "259:3",
	}
	areas := map[string]bool{
		"/swap.img": true, "/var/swap file": false, "/srv/back\\slash": false, "/dev/dm-2": true, "/dev/dm-3": true,
		"/dev/dm-4": false, "/dev/dm-5p1": false, "/dev/bcache0": true, "/dev/zram0": false, "/dev/zram1": false,
		"/dev/zram2": true, "/dev/zram3": false, "/dev/loop0": true, "/srv/btrfs/swap": true, "/swap.gone": true,
		"/dev/nvme0n1p3": true,
	}
	var table strings.Builder
	table.WriteString("Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n")
	var want []string
	for path, exposed := range areas {
		fmt.Fprintf(&table, "%s\tfile\t\t4194300\t\t0\t\t-2\n", escape(path))
		if exposed {
			want = append(want, path)
		}
	}
	tree.write("proc/swaps", table.String())
	tree.write("proc/self/status", "")
	exposed, err := clearSwap(
		filepath.Join(tree.root, "proc", "swaps"),
		tree.devices(),
		func(path string) (string, error) {
			if number, ok := numbers[path]; ok {
				return number, nil
			}
			return "", errors.New("no such file")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(exposed)
	slices.Sort(want)
	if !slices.Equal(exposed, want) {
		t.Fatalf("exposed swap areas: got %q, want %q", exposed, want)
	}
}

func TestClearSwapWithoutTheSwapTable(t *testing.T) {
	proc := t.TempDir()
	if _, err := clearSwap(filepath.Join(proc, "swaps"), t.TempDir(), blockDevice); !errors.Is(err, errNoProc) {
		t.Fatalf("without /proc nylzo cannot tell, got %v", err)
	}
	if err := os.Mkdir(filepath.Join(proc, "self"), 0o700); err != nil {
		t.Fatal(err)
	}
	if exposed, err := clearSwap(filepath.Join(proc, "swaps"), t.TempDir(), blockDevice); exposed != nil || err != nil {
		t.Fatalf("a kernel without /proc/swaps has no swap, got %q, %v", exposed, err)
	}
}
