package harden

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

// TestProtectHidesTheProcessFromTheSameUser covers F07: another process of the same user can still wait for and end
// a protected process, but can neither read its memory nor change its DACL to grant itself that right.
func TestProtectHidesTheProcessFromTheSameUser(t *testing.T) {
	plain, _ := startChild(t, "plain")
	if err := open(plain.Process.Pid, windows.PROCESS_VM_READ); err != nil {
		t.Skipf("this host denies memory reads of an unprotected child already: %v", err)
	}
	child, report := startChild(t, "full")
	if report.Warning != "" {
		t.Errorf("Windows has no swap warning, got %s", report.Warning)
	}
	for access, allowed := range map[uint32]bool{
		windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.SYNCHRONIZE: true,
		windows.PROCESS_TERMINATE:                                   true,
		windows.PROCESS_VM_READ:                                     false,
		windows.PROCESS_QUERY_INFORMATION | windows.PROCESS_VM_READ: false,
		windows.PROCESS_DUP_HANDLE:                                  false,
		windows.WRITE_DAC:                                           false,
		windows.WRITE_OWNER:                                         false,
	} {
		err := open(child.Process.Pid, access)
		if allowed && err != nil {
			t.Errorf("access %#x to the protected child: %v", access, err)
		}
		if !allowed && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Errorf("access %#x to the protected child: want access denied, got %v", access, err)
		}
	}
}

func open(pid int, access uint32) error {
	handle, err := windows.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return err
	}
	return windows.CloseHandle(handle)
}
