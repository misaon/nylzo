package harden

import (
	"errors"

	"golang.org/x/sys/windows"
)

const (
	userAccess  = windows.SYNCHRONIZE | windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.PROCESS_TERMINATE
	ownerAccess = windows.READ_CONTROL
)

var (
	errMemoryLockLinuxOnly = errors.New("nylzo locks its memory against swap only on Linux, not on Windows")
	isDebuggerPresent      = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsDebuggerPresent")
)

// disableDumps replaces the process DACL, so a process of the same user that opens nylzo by its id can neither read
// the memory (PROCESS_VM_READ, which MiniDumpWriteDump needs too) nor give that right back: the OWNER RIGHTS entry
// takes away the owner's implicit WRITE_DAC. Handles opened before, such as the one the parent shell holds, keep
// their rights, and so do the threads, which Windows gives the token's default DACL.
func disableDumps() error {
	if err := restrictAccess(); err != nil {
		return err
	}
	if present, _, _ := isDebuggerPresent.Call(); present != 0 {
		return errTraced
	}
	return nil
}

func restrictAccess() error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	owner, err := windows.CreateWellKnownSid(windows.WinCreatorOwnerRightsSid)
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		allow(userAccess, windows.TRUSTEE_IS_USER, user.User.Sid),
		allow(ownerAccess, windows.TRUSTEE_IS_WELL_KNOWN_GROUP, owner),
	}, nil)
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(
		windows.CurrentProcess(),
		windows.SE_KERNEL_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	)
}

func allow(access windows.ACCESS_MASK, kind windows.TRUSTEE_TYPE, sid *windows.SID) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: access,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  kind,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}

func LockMemory() error {
	return errMemoryLockLinuxOnly
}

// keepFromSwap has nothing to do: Go offers no way to lock its heap on Windows, and only an administrator can tell
// whether BitLocker encrypts the page file.
func keepFromSwap() string {
	return ""
}
