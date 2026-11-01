package operator

import "syscall"

const lockOnFault = 4

// hardenProcess keeps record keys and the pepper out of core files and out of reach of other processes of the user.
func hardenProcess() error {
	if err := syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{}); err != nil {
		return err
	}
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0); errno != 0 {
		return errno
	}
	return nil
}

// lockMemory keeps every page the process maps out of swap; MCL_ONFAULT locks a page only once it is used.
func lockMemory() error {
	return syscall.Mlockall(syscall.MCL_CURRENT | syscall.MCL_FUTURE | lockOnFault)
}
