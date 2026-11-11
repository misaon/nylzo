package harden

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// TestProtectHidesTheProcessFromTheSameUser covers F07: a protected process writes no core file, and another
// process of the same user can neither read its memory and environment nor attach a debugger.
func TestProtectHidesTheProcessFromTheSameUser(t *testing.T) {
	if mayTrace() {
		t.Skip("CAP_SYS_PTRACE reads and traces any process, protected or not")
	}
	plain, _ := startChild(t, "plain")
	if _, err := os.ReadFile(procFile(plain.Process.Pid, "environ")); err != nil {
		t.Skipf("this host hides an unprotected child already: %v", err)
	}
	for _, mode := range []string{"process", "full"} {
		t.Run(mode, func(t *testing.T) {
			child, report := startChild(t, mode)
			if report.Facts["core_soft"] != 0 || report.Facts["core_hard"] != 0 {
				t.Errorf("the core file limit stayed %d/%d", report.Facts["core_soft"], report.Facts["core_hard"])
			}
			for _, name := range []string{"environ", "mem"} {
				if _, err := os.ReadFile(procFile(child.Process.Pid, name)); !errors.Is(err, fs.ErrPermission) {
					t.Errorf("/proc/<pid>/%s of the protected child: want permission denied, got %v", name, err)
				}
			}
			if err := attach(child.Process.Pid); !errors.Is(err, syscall.EPERM) {
				t.Errorf("ptrace attach to the protected child: want EPERM, got %v", err)
			}
		})
	}
}

// TestFullProtectionWarnsOnlyWithoutTheLock checks the two outcomes a host can give: the memory is locked and
// nothing is said, or it is not and only clear swap areas are named.
func TestFullProtectionWarnsOnlyWithoutTheLock(t *testing.T) {
	_, report := startChild(t, "full")
	if report.Facts["locked_kib"] > 0 && report.Warning != "" {
		t.Fatalf("the child locked its memory and still warned: %s", report.Warning)
	}
	exposed, err := clearSwap(procSwaps, sysBlock, blockDevice)
	if report.Facts["locked_kib"] == 0 && (report.Warning != "") != (err != nil || len(exposed) > 0) {
		t.Fatalf("the unlocked child warned %q for the swap areas %q (%v)", report.Warning, exposed, err)
	}
}

// TestAProcessUnderATracerIsRefused covers the F07 review: a tracer may hold any thread, not just the first, and
// without /proc nylzo cannot tell.
func TestAProcessUnderATracerIsRefused(t *testing.T) {
	threads := t.TempDir()
	for thread, tracer := range map[string]string{"100": "0", "101": "0"} {
		if err := os.MkdirAll(filepath.Join(threads, thread), 0o700); err != nil {
			t.Fatal(err)
		}
		status := "Name:\tnylzo\nTracerPid:\t" + tracer + "\nUid:\t1000\n"
		if err := os.WriteFile(filepath.Join(threads, thread, "status"), []byte(status), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := refuseTracers(threads); err != nil {
		t.Fatalf("no thread is traced, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(threads, "101", "status"), []byte("TracerPid:\t4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := refuseTracers(threads); !errors.Is(err, errTraced) {
		t.Fatalf("a traced second thread must be refused, got %v", err)
	}
	if err := refuseTracers(filepath.Join(threads, "missing")); !errors.Is(err, errTracerUnknown) {
		t.Fatalf("without /proc the process must not go on, got %v", err)
	}
}

func mayTrace() bool {
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var capabilities [2]unix.CapUserData
	const bitsPerWord = 32
	return unix.Capget(&header, &capabilities[0]) == nil &&
		capabilities[unix.CAP_SYS_PTRACE/bitsPerWord].Effective&(1<<(unix.CAP_SYS_PTRACE%bitsPerWord)) != 0
}

func procFile(pid int, name string) string {
	return fmt.Sprintf("/proc/%d/%s", pid, name)
}

// attach must stay on one thread, which ptrace requires of a tracer, and detaches at once if the kernel let it in.
func attach(pid int) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err := unix.PtraceAttach(pid)
	if err == nil {
		var status unix.WaitStatus
		_, _ = unix.Wait4(pid, &status, 0, nil)
		_ = unix.PtraceDetach(pid)
	}
	return err
}
