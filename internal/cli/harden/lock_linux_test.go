package harden

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

const (
	lockChildVariable   = "NYLZO_TEST_LOCK_MEMORY_CHILD"
	finiteChildVariable = "NYLZO_TEST_FINITE_LOCK_CHILD"
)

// TestLockMemoryKeepsThePagesOutOfSwap covers A4-2; a child process locks itself, so the test binary stays unlocked.
func TestLockMemoryKeepsThePagesOutOfSwap(t *testing.T) {
	if os.Getenv(lockChildVariable) == "1" {
		var limit unix.Rlimit
		if unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit) == nil {
			unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{Cur: limit.Max, Max: limit.Max})
		}
		if err := LockMemory(); err != nil {
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
// small, and the process would stop once the runtime maps past it. A child lowers its hard limit, which it cannot undo.
func TestAFiniteMemoryLockLimitIsRefused(t *testing.T) {
	if os.Getenv(finiteChildVariable) == "1" {
		var limit unix.Rlimit
		if err := unix.Getrlimit(unix.RLIMIT_MEMLOCK, &limit); err != nil {
			fmt.Fprintln(os.Stdout, "getrlimit:", err)
			os.Exit(1)
		}
		finite := min(limit.Max, 64<<20)
		if err := unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{Cur: finite, Max: finite}); err != nil {
			fmt.Fprintln(os.Stdout, "setrlimit:", err)
			os.Exit(1)
		}
		err := LockMemory()
		fmt.Fprintln(os.Stdout, "refused:", errors.Is(err, errFiniteMemoryLock), "locked:", err == nil)
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestAFiniteMemoryLockLimitIsRefused$")
	child.Env = append(os.Environ(), finiteChildVariable+"=1")
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the child failed: %v %s", err, output)
	}
	want := "refused: true locked: false"
	if hasInitialIPCLock(t) {
		want = "refused: false locked: true"
	}
	if !bytes.Contains(output, []byte(want)) {
		t.Fatalf("a finite memlock limit: want %q, got %s", want, output)
	}
}

func hasInitialIPCLock(t *testing.T) bool {
	t.Helper()
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
	var namespace unix.Stat_t
	return capabilities&(1<<unix.CAP_IPC_LOCK) != 0 &&
		unix.Stat("/proc/self/ns/user", &namespace) == nil && namespace.Ino == initialUserNamespace
}
