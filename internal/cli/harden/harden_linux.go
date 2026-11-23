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

type kernel struct {
	threads   string
	swaps     string
	blocks    string
	device    func(path string) (string, error)
	getrlimit func(resource int, limit *unix.Rlimit) error
	setrlimit func(resource int, limit *unix.Rlimit) error
	prctl     func(option int, arg2, arg3, arg4, arg5 uintptr) error
	capget    func(header *unix.CapUserHeader, data *unix.CapUserData) error
	stat      func(path string, status *unix.Stat_t) error
	mlockall  func(flags int) error
	sysinfo   func(info *unix.Sysinfo_t) error
}

var thisKernel = kernel{
	threads:   threadsDirectory,
	swaps:     procSwaps,
	blocks:    sysBlock,
	device:    blockDevice,
	getrlimit: unix.Getrlimit,
	setrlimit: unix.Setrlimit,
	prctl:     unix.Prctl,
	capget:    unix.Capget,
	stat:      unix.Stat,
	mlockall:  unix.Mlockall,
	sysinfo:   unix.Sysinfo,
}

var (
	disableDumps = thisKernel.disableDumps
	keepFromSwap = thisKernel.keepFromSwap
	// LockMemory keeps every page the process maps out of swap; MCL_ONFAULT locks a page only once it is used.
	LockMemory = thisKernel.lockMemory
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
func (k *kernel) disableDumps() error {
	if err := k.setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		return err
	}
	if err := k.prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	return refuseTracers(k.threads)
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

func (k *kernel) lockMemory() error {
	k.raiseLockLimit()
	if err := k.requireUnlimitedLock(); err != nil {
		return err
	}
	return k.mlockall(lockFlags)
}

// keepFromSwap locks the memory when no later mapping can fail for it; otherwise it names the swap that would keep
// the pages in clear.
func (k *kernel) keepFromSwap() string {
	lockErr := k.lockMemory()
	if lockErr == nil {
		return ""
	}
	exposed, err := clearSwap(k.swaps, k.blocks, k.device)
	if errors.Is(err, errNoSwapTable) && !k.swapActive() {
		return ""
	}
	return swapWarning(exposed, err, lockErr)
}

// swapActive asks the kernel directly, since /proc/swaps can be hidden (ProcSubset=pid) or not mounted.
func (k *kernel) swapActive() bool {
	var info unix.Sysinfo_t
	return k.sysinfo(&info) != nil || info.Totalswap > 0
}

func (k *kernel) raiseLockLimit() {
	var limit unix.Rlimit
	if k.getrlimit(unix.RLIMIT_MEMLOCK, &limit) == nil && limit.Cur < limit.Max {
		_ = k.setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{Cur: limit.Max, Max: limit.Max})
	}
}

// requireUnlimitedLock refuses a finite memlock limit, which mlockall accepts while the heap is small; MCL_FUTURE then
// makes a later mapping past the limit fail and the process stop. CAP_IPC_LOCK lifts the limit only in the initial
// user namespace, so a container's own capability does not count.
func (k *kernel) requireUnlimitedLock() error {
	var limit unix.Rlimit
	if err := k.getrlimit(unix.RLIMIT_MEMLOCK, &limit); err != nil {
		return err
	}
	if limit.Cur == unix.RLIM_INFINITY {
		return nil
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var capabilities [2]unix.CapUserData
	if err := k.capget(&header, &capabilities[0]); err != nil {
		return err
	}
	const bitsPerWord = 32
	held := capabilities[unix.CAP_IPC_LOCK/bitsPerWord].Effective&(1<<(unix.CAP_IPC_LOCK%bitsPerWord)) != 0
	var namespace unix.Stat_t
	if held && k.stat("/proc/self/ns/user", &namespace) == nil && namespace.Ino == initialUserNamespace {
		return nil
	}
	return errFiniteMemoryLock
}
