package harden

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

const (
	// swapUsageEncrypted is the offset of xsu_encrypted in struct xsw_usage of <sys/sysctl.h>.
	swapUsageEncrypted = 28
	swapUsageSize      = 32
	// processTraced is P_TRACED of <sys/proc.h>.
	processTraced = 0x00000800
)

var errMemoryLockLinuxOnly = errors.New("nylzo locks its memory against swap only on Linux, not on macOS")

// disableDumps also stops ptrace attach. PT_DENY_ATTACH makes a traced process exit with ENOTSUP, so a debugger
// that is already attached is refused first with a message.
func disableDumps() error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		return err
	}
	process, err := unix.SysctlKinfoProc("kern.proc.pid", unix.Getpid())
	if err != nil {
		return err
	}
	if process.Proc.P_flag&processTraced != 0 {
		return errTraced
	}
	return unix.PtraceDenyAttach()
}

func LockMemory() error {
	return errMemoryLockLinuxOnly
}

// keepFromSwap checks that macOS encrypts swap, which it does on every supported release.
func keepFromSwap() string {
	usage, err := unix.SysctlRaw("vm.swapusage")
	switch {
	case err != nil:
		return "nylzo could not check whether macOS encrypts swap: " + err.Error()
	case len(usage) < swapUsageSize:
		return "nylzo could not check whether macOS encrypts swap: vm.swapusage is too short"
	case binary.NativeEndian.Uint32(usage[swapUsageEncrypted:]) == 0:
		return "macOS reports swap as not encrypted, so a secret could reach the disk"
	default:
		return ""
	}
}
