package operator

import (
	"errors"

	"golang.org/x/sys/unix"
)

var errFiniteMemoryLock = errors.New(
	"the memlock limit is finite and the process lacks CAP_IPC_LOCK, so memory the runtime maps later could not be locked",
)

// hardenProcess keeps record keys and the pepper out of core files and out of reach of other processes of the user.
func hardenProcess() error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		return err
	}
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}

// lockMemory keeps every page the process maps out of swap; MCL_ONFAULT locks a page only once it is used.
func lockMemory() error {
	if err := requireUnlimitedLock(); err != nil {
		return err
	}
	return unix.Mlockall(unix.MCL_CURRENT | unix.MCL_FUTURE | unix.MCL_ONFAULT)
}

// requireUnlimitedLock refuses a finite memlock limit, which mlockall accepts while the heap is small; MCL_FUTURE then
// makes a later mapping past the limit fail and the server stop under load.
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
	if capabilities[unix.CAP_IPC_LOCK/32].Effective&(1<<(unix.CAP_IPC_LOCK%32)) != 0 {
		return nil
	}
	return errFiniteMemoryLock
}
