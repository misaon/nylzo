//go:build linux || darwin

package harden

import (
	"os"
	"regexp"
	"strconv"

	"golang.org/x/sys/unix"
)

var lockedMemory = regexp.MustCompile(`(?m)^VmLck:\s+(\d+) kB$`)

func ownFacts() map[string]uint64 {
	facts := map[string]uint64{}
	var core unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_CORE, &core) == nil {
		facts["core_soft"], facts["core_hard"] = core.Cur, core.Max
	}
	status, err := os.ReadFile("/proc/self/status")
	if match := lockedMemory.FindSubmatch(status); err == nil && match != nil {
		facts["locked_kib"], _ = strconv.ParseUint(string(match[1]), 10, 64)
	}
	return facts
}
