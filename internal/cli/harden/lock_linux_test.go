package harden

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	lockChildVariable   = "NYLZO_TEST_LOCK_MEMORY_CHILD"
	finiteChildVariable = "NYLZO_TEST_FINITE_LOCK_CHILD"
)

// A child process locks itself, so the test binary stays unlocked.
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

// mlockall accepts a finite limit while the heap is small, and the process would stop once the runtime maps past it.
// A child lowers its hard limit, which it cannot undo.
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

type fakeKernel struct {
	kernel

	limit  unix.Rlimit
	locked int
}

func newFakeKernel(t *testing.T) *fakeKernel {
	t.Helper()
	fake := &fakeKernel{limit: unix.Rlimit{Cur: 64 << 10, Max: 64 << 10}}
	fake.kernel = kernel{
		threads: t.TempDir(),
		swaps:   filepath.Join(t.TempDir(), "swaps"),
		blocks:  t.TempDir(),
		device:  func(string) (string, error) { return "", os.ErrNotExist },
		getrlimit: func(_ int, limit *unix.Rlimit) error {
			*limit = fake.limit
			return nil
		},
		setrlimit: func(resource int, limit *unix.Rlimit) error {
			if resource == unix.RLIMIT_MEMLOCK {
				fake.limit = *limit
			}
			return nil
		},
		prctl:    func(int, uintptr, uintptr, uintptr, uintptr) error { return nil },
		capget:   func(*unix.CapUserHeader, *unix.CapUserData) error { return nil },
		stat:     func(string, *unix.Stat_t) error { return nil },
		mlockall: func(flags int) error { fake.locked = flags; return nil },
		sysinfo:  func(*unix.Sysinfo_t) error { return nil },
	}
	return fake
}

func (f *fakeKernel) holdIPCLock(namespace uint64) {
	f.capget = func(_ *unix.CapUserHeader, data *unix.CapUserData) error {
		data.Effective = 1 << unix.CAP_IPC_LOCK
		return nil
	}
	f.stat = func(_ string, status *unix.Stat_t) error {
		status.Ino = namespace
		return nil
	}
}

// Only an unlimited memlock limit, or CAP_IPC_LOCK of the initial user namespace, lets every later mapping be locked.
func TestMemoryIsLockedOnlyWhenNoLaterMappingCanFail(t *testing.T) {
	refused := errors.New("refused")
	for name, testCase := range map[string]struct {
		change func(*fakeKernel)
		want   error
	}{
		"a finite limit": {func(*fakeKernel) {}, errFiniteMemoryLock},
		"a soft limit below an unlimited hard one": {func(fake *fakeKernel) {
			fake.limit = unix.Rlimit{Cur: 64 << 10, Max: unix.RLIM_INFINITY}
		}, nil},
		"CAP_IPC_LOCK of the initial namespace": {func(fake *fakeKernel) {
			fake.holdIPCLock(initialUserNamespace)
		}, nil},
		"CAP_IPC_LOCK of a container namespace": {func(fake *fakeKernel) {
			fake.holdIPCLock(initialUserNamespace - 1)
		}, errFiniteMemoryLock},
		"an unreadable limit": {func(fake *fakeKernel) {
			fake.getrlimit = func(int, *unix.Rlimit) error { return refused }
		}, refused},
		"unreadable capabilities": {func(fake *fakeKernel) {
			fake.capget = func(*unix.CapUserHeader, *unix.CapUserData) error { return refused }
		}, refused},
		"a refused mlockall": {func(fake *fakeKernel) {
			fake.limit = unix.Rlimit{Cur: unix.RLIM_INFINITY, Max: unix.RLIM_INFINITY}
			fake.mlockall = func(int) error { return refused }
		}, refused},
	} {
		fake := newFakeKernel(t)
		testCase.change(fake)
		err := fake.lockMemory()
		if !errors.Is(err, testCase.want) || testCase.want == nil && err != nil {
			t.Errorf("%s: got %v, want %v", name, err, testCase.want)
		}
		if locked := fake.locked == lockFlags; locked != (err == nil) {
			t.Errorf("%s: mlockall ran %v while the lock returned %v", name, locked, err)
		}
	}
}

func TestDumpsStayOnWhenTheKernelRefusesToTurnThemOff(t *testing.T) {
	refused := errors.New("refused")
	granted := newFakeKernel(t)
	if err := granted.disableDumps(); err != nil {
		t.Fatalf("a kernel that grants every call: %v", err)
	}
	coreLimit := newFakeKernel(t)
	coreLimit.setrlimit = func(int, *unix.Rlimit) error { return refused }
	dumpable := newFakeKernel(t)
	dumpable.prctl = func(int, uintptr, uintptr, uintptr, uintptr) error { return refused }
	for _, fake := range []*fakeKernel{coreLimit, dumpable} {
		if err := fake.disableDumps(); !errors.Is(err, refused) {
			t.Errorf("a refused call must stop the protection, got %v", err)
		}
	}
}

// A hidden swap table counts as clear swap while the kernel reports swap or cannot be asked.
func TestFullProtectionNamesOnlySwapThatKeepsPagesInClear(t *testing.T) {
	refused := errors.New("refused")
	locked := newFakeKernel(t)
	locked.limit = unix.Rlimit{Cur: unix.RLIM_INFINITY, Max: unix.RLIM_INFINITY}
	if warning := locked.keepFromSwap(); warning != "" {
		t.Errorf("locked memory warned: %s", warning)
	}
	noSwap := newFakeKernel(t)
	if warning := noSwap.keepFromSwap(); warning != "" {
		t.Errorf("no swap warned: %s", warning)
	}
	hiddenSwap := newFakeKernel(t)
	hiddenSwap.sysinfo = func(info *unix.Sysinfo_t) error {
		info.Totalswap = 1 << 30
		return nil
	}
	silentKernel := newFakeKernel(t)
	silentKernel.sysinfo = func(*unix.Sysinfo_t) error { return refused }
	for name, fake := range map[string]*fakeKernel{"hidden swap": hiddenSwap, "no sysinfo": silentKernel} {
		if warning := fake.keepFromSwap(); !strings.Contains(warning, errNoSwapTable.Error()) {
			t.Errorf("%s: %q", name, warning)
		}
	}
	listed := newFakeKernel(t)
	listed.mlockall = func(int) error { return syscall.EPERM }
	listed.limit = unix.Rlimit{Cur: unix.RLIM_INFINITY, Max: unix.RLIM_INFINITY}
	if err := os.WriteFile(listed.swaps, []byte("/swap.img file 4194300 0 -2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if warning := listed.keepFromSwap(); !strings.Contains(warning, "/swap.img") ||
		!strings.Contains(warning, syscall.EPERM.Error()) {
		t.Errorf("a swap file in clear: %q", warning)
	}
}
