package harden

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	procSwaps      = "/proc/swaps"
	sysBlock       = "/sys/dev/block"
	maxStackDepth  = 16
	octalEscapeLen = 4
)

// cryptTypes are the libcryptsetup device types that encrypt; INTEGRITY, VERITY and SUBDEV devices do not.
var cryptTypes = []string{"LUKS1", "LUKS2", "PLAIN", "LOOPAES", "TCRYPT", "BITLK", "FVAULT2"}

var errNoProc = errors.New("/proc is not mounted")

func swapWarning(areas []string, checkErr, lockErr error) string {
	if checkErr == nil && len(areas) == 0 {
		return ""
	}
	reason, advice := "the memlock limit is finite", "encrypt the swap or raise the memlock limit to unlimited"
	if !errors.Is(lockErr, errFiniteMemoryLock) {
		reason, advice = lockErr.Error(), "encrypt the swap"
	}
	if checkErr != nil {
		return fmt.Sprintf(
			"this command could not lock its memory (%s) or check whether swap is encrypted (%v); %s",
			reason,
			checkErr,
			advice,
		)
	}
	return fmt.Sprintf(
		"this command could not lock its memory (%s), and swap on %s is not encrypted, so a secret could reach "+
			"that disk; %s",
		reason,
		strings.Join(areas, ", "),
		advice,
	)
}

// clearSwap lists the active swap areas whose pages could reach a disk in clear. An area counts as encrypted only
// when dm-crypt sits under every path of its block device stack; zram keeps its pages in memory unless it writes them
// back to a device.
func clearSwap(table, devices string, device func(string) (string, error)) ([]string, error) {
	raw, err := os.ReadFile(table)
	if errors.Is(err, os.ErrNotExist) {
		if _, procErr := os.Stat(filepath.Join(filepath.Dir(table), "self")); procErr != nil {
			return nil, errNoProc
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stack := blockStack{devices: devices, device: device}
	var exposed []string
	for _, area := range swapAreas(string(raw)) {
		if stack.keepsInClear(area, 0) {
			exposed = append(exposed, area)
		}
	}
	return exposed, nil
}

type blockStack struct {
	devices string
	device  func(string) (string, error)
}

// keepsInClear tells whether a page written to the device or file at path could reach a disk in clear; whatever
// cannot be traced counts as clear.
func (s blockStack) keepsInClear(path string, depth int) bool {
	if depth > maxStackDepth {
		return true
	}
	number, err := s.device(path)
	if err != nil {
		return true
	}
	node, err := filepath.EvalSymlinks(filepath.Join(s.devices, number))
	if err != nil {
		return true
	}
	if !strings.HasPrefix(filepath.Base(node), "zram") {
		return !encrypted(node, 0)
	}
	backing, readErr := os.ReadFile(filepath.Join(node, "backing_dev"))
	target := strings.TrimSpace(string(backing))
	switch {
	case errors.Is(readErr, os.ErrNotExist), readErr == nil && target == "none":
		return false
	case readErr != nil:
		return true
	default:
		return s.keepsInClear(target, depth+1)
	}
}

func swapAreas(table string) []string {
	var areas []string
	for line := range strings.Lines(table) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "Filename" {
			continue
		}
		areas = append(areas, unescapeOctal(fields[0]))
	}
	return areas
}

// unescapeOctal undoes the \ooo escapes the kernel writes for space, tab, newline and backslash in a path.
func unescapeOctal(path string) string {
	var out strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] == '\\' && i+octalEscapeLen <= len(path) {
			if value, err := strconv.ParseUint(path[i+1:i+octalEscapeLen], 8, 8); err == nil {
				out.WriteByte(byte(value))
				i += octalEscapeLen - 1
				continue
			}
		}
		out.WriteByte(path[i])
	}
	return out.String()
}

// encrypted follows the lower devices that sysfs lists; bcache also writes to a cache device it does not list, so
// it counts as clear.
func encrypted(node string, depth int) bool {
	if depth > maxStackDepth || strings.HasPrefix(filepath.Base(node), "bcache") {
		return false
	}
	if uuid, err := os.ReadFile(filepath.Join(node, "dm", "uuid")); err == nil && cryptUUID(string(uuid)) {
		return true
	}
	if _, err := os.Stat(filepath.Join(node, "partition")); err == nil {
		return encrypted(filepath.Dir(node), depth+1)
	}
	lower := filepath.Join(node, "slaves")
	entries, err := os.ReadDir(lower)
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		target, linkErr := filepath.EvalSymlinks(filepath.Join(lower, entry.Name()))
		if linkErr != nil || !encrypted(target, depth+1) {
			return false
		}
	}
	return true
}

// cryptUUID reads the CRYPT-<type>-<uuid>-<name> device-mapper UUID that libcryptsetup gives its devices.
func cryptUUID(uuid string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(uuid), "CRYPT-")
	kind, _, _ := strings.Cut(rest, "-")
	return ok && slices.Contains(cryptTypes, kind)
}

// blockDevice names the major:minor of a swap partition, or of the device under the file system of a swap file.
func blockDevice(path string) (string, error) {
	var status unix.Stat_t
	if err := unix.Stat(path, &status); err != nil {
		return "", err
	}
	number := status.Dev
	if status.Mode&unix.S_IFMT == unix.S_IFBLK {
		number = status.Rdev
	}
	return fmt.Sprintf("%d:%d", unix.Major(number), unix.Minor(number)), nil
}
