package harden

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const lockFlags = unix.MCL_CURRENT | unix.MCL_FUTURE | unix.MCL_ONFAULT

const (
	threadsDirectory = "/proc/self/task"
	// initialUserNamespace is PROC_USER_INIT_INO, the inode of /proc/self/ns/user in the initial user namespace.
	initialUserNamespace = 0xEFFFFFFD
)

var (
	errFiniteMemoryLock = errors.New(
		"the memlock limit is finite and the process lacks CAP_IPC_LOCK, so memory the runtime maps later could not be locked",
	)
	errTracerUnknown = errors.New("nylzo cannot see whether a tracer is attached")
	errNoSwapTable   = errors.New("/proc/swaps cannot be read while swap is active")
)

// disableDumps also makes /proc/<pid> root-owned, so no other process of the user reads the memory or environment.
// A tracer that attached before keeps its access to the thread it traces, so the process refuses to go on while any
// of its threads has one.
func disableDumps() error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	return refuseTracers(threadsDirectory)
}

// refuseTracers fails closed when /proc cannot show the threads.
func refuseTracers(threads string) error {
	entries, err := os.ReadDir(threads)
	if err != nil {
		return fmt.Errorf("%w: %w", errTracerUnknown, err)
	}
	for _, entry := range entries {
		status, readErr := os.ReadFile(filepath.Join(threads, entry.Name(), "status"))
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return fmt.Errorf("%w: %w", errTracerUnknown, readErr)
		}
		if tracedErr := refuseTracer(status); tracedErr != nil {
			return tracedErr
		}
	}
	return nil
}

func refuseTracer(status []byte) error {
	for line := range bytes.Lines(status) {
		if pid, ok := bytes.CutPrefix(line, []byte("TracerPid:")); ok && string(bytes.TrimSpace(pid)) != "0" {
			return fmt.Errorf("%w (process %s)", errTraced, bytes.TrimSpace(pid))
		}
	}
	return nil
}

// LockMemory keeps every page the process maps out of swap; MCL_ONFAULT locks a page only once it is used.
func LockMemory() error {
	raiseLockLimit()
	if err := requireUnlimitedLock(); err != nil {
		return err
	}
	return unix.Mlockall(lockFlags)
}

// keepFromSwap locks the memory when no later mapping can fail for it; otherwise it names the swap that would keep
// the pages in clear.
func keepFromSwap() string {
	lockErr := LockMemory()
	if lockErr == nil {
		return ""
	}
	exposed, err := clearSwap(procSwaps, sysBlock, blockDevice)
	if errors.Is(err, errNoSwapTable) && !swapActive() {
		return ""
	}
	return swapWarning(exposed, err, lockErr)
}

// swapActive asks the kernel directly, since /proc/swaps can be hidden (ProcSubset=pid) or not mounted.
func swapActive() bool {
	var info unix.Sysinfo_t
	return unix.Sysinfo(&info) != nil || info.Totalswap > 0
}

func raiseLockLimit() {
	var limit unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit) == nil && limit.Cur < limit.Max {
		_ = unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{Cur: limit.Max, Max: limit.Max})
	}
}

// requireUnlimitedLock refuses a finite memlock limit, which mlockall accepts while the heap is small; MCL_FUTURE then
// makes a later mapping past the limit fail and the process stop. CAP_IPC_LOCK lifts the limit only in the initial
// user namespace, so a container's own capability does not count.
func requireUnlimitedLock() error {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit); err != nil {
		return err
	}
	if limit.Cur == unix.RLIM_INFINITY {
		return nil
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var capabilities [2]unix.CapUserData
	if err := unix.Capget(&header, &capabilities[0]); err != nil {
		return err
	}
	const bitsPerWord = 32
	held := capabilities[unix.CAP_IPC_LOCK/bitsPerWord].Effective&(1<<(unix.CAP_IPC_LOCK%bitsPerWord)) != 0
	var namespace unix.Stat_t
	if held && unix.Stat("/proc/self/ns/user", &namespace) == nil && namespace.Ino == initialUserNamespace {
		return nil
	}
	return errFiniteMemoryLock
}
