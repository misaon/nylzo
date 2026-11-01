package operator

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"testing"
)

const lockChildVariable = "NYLZO_TEST_LOCK_MEMORY_CHILD"

// TestLockMemoryKeepsThePagesOutOfSwap covers A4-2; a child process locks itself, so the test binary stays unlocked.
func TestLockMemoryKeepsThePagesOutOfSwap(t *testing.T) {
	if os.Getenv(lockChildVariable) == "1" {
		if err := lockMemory(); err != nil {
			fmt.Fprintln(os.Stderr, "lock:", errors.Is(err, syscall.ENOMEM) || errors.Is(err, syscall.EPERM), err)
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
		t.Skip("the memlock limit of this host is below what the test binary maps: " + string(output))
	}
	if err != nil {
		t.Fatalf("the child could not lock its memory: %v %s", err, output)
	}
	locked := regexp.MustCompile(`(?m)^VmLck:\s+(\d+) kB$`).FindSubmatch(output)
	if locked == nil || string(locked[1]) == "0" {
		t.Fatalf("the process locked no memory: %s", output)
	}
}
