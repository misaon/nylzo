package operator

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const lockChildVariable = "NYLZO_TEST_LOCK_MEMORY_CHILD"

// TestLockMemoryKeepsThePagesOutOfSwap covers A4-2; a child process locks itself, so the test binary stays unlocked.
func TestLockMemoryKeepsThePagesOutOfSwap(t *testing.T) {
	if os.Getenv(lockChildVariable) == "1" {
		var limit unix.Rlimit
		if unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit) == nil {
			unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{Cur: limit.Max, Max: limit.Max})
		}
		if err := lockMemory(); err != nil {
			limited := errors.Is(err, syscall.ENOMEM) || errors.Is(err, syscall.EPERM) ||
				errors.Is(err, errFiniteMemoryLock)
			fmt.Fprintln(os.Stderr, "lock:", limited, err)
			os.Exit(3)
		}
		status, _ := os.ReadFile("/proc/self/status")
		os.Stdout.Write(status)
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestLockMemoryKeepsThePagesOutOfSwap$")
	child.Env = append(os.Environ(), lockChildVariable+"=1")
	output, err := child.CombinedOutput()
	if bytes.Contains(output, []byte("lock: true")) {
		t.Skip("this host allows no unlimited memory lock: " + string(output))
	}
	if err != nil {
		t.Fatalf("the child could not lock its memory: %v %s", err, output)
	}
	locked := regexp.MustCompile(`(?m)^VmLck:\s+(\d+) kB$`).FindSubmatch(output)
	if len(locked) < 2 || string(locked[1]) == "0" {
		t.Fatalf("the process locked no memory: %s", output)
	}
}

// TestAFiniteMemoryLockLimitIsRefused covers the final review's L5: mlockall accepts a finite limit while the heap is
// small, and the server would stop once the runtime maps past it.
func TestAFiniteMemoryLockLimitIsRefused(t *testing.T) {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit); err != nil {
		t.Fatal(err)
	}
	finite := unix.Rlimit{Cur: min(limit.Max, 64<<20), Max: limit.Max}
	if err := unix.Setrlimit(unix.RLIMIT_MEMLOCK, &finite); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Setrlimit(unix.RLIMIT_MEMLOCK, &limit) })
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	effective := regexp.MustCompile(`(?m)^CapEff:\s+([0-9a-f]+)$`).FindSubmatch(status)
	if len(effective) < 2 {
		t.Fatalf("no effective capabilities in %s", status)
	}
	capabilities, err := strconv.ParseUint(string(effective[1]), 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	if capabilities&(1<<unix.CAP_IPC_LOCK) != 0 {
		if err = requireUnlimitedLock(); err != nil {
			t.Fatalf("CAP_IPC_LOCK ignores the limit, yet the lock was refused: %v", err)
		}
		return
	}
	if err = lockMemory(); !errors.Is(err, errFiniteMemoryLock) {
		t.Fatalf("a finite memlock limit without CAP_IPC_LOCK must be refused before mlockall, got %v", err)
	}
}
