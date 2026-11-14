package cli

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// The kernel drops a signal that the init process of a container did not catch, so there read exits with 128 plus
// the signal number instead, the status docker reports for a process a signal ended.
func TestTheInitProcessOfAContainerExitsWithTheSignalStatus(t *testing.T) {
	t.Parallel()
	asInit := func(process *exec.Cmd) {
		process.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags:  syscall.CLONE_NEWUSER | syscall.CLONE_NEWPID,
			UidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getuid(), HostID: os.Getuid(), Size: 1}},
			GidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getgid(), HostID: os.Getgid(), Size: 1}},
		}
	}
	probe := exec.CommandContext(t.Context(), "/bin/sh", "-c", `test "$$" = 1`)
	asInit(probe)
	if err := probe.Run(); err != nil {
		t.Skipf("cannot start a process as the init of a new PID namespace: %v", err)
	}
	stopped := stopRead(t, stopCase{atClaim: true, answer: true, prepare: asInit}, syscall.SIGTERM)
	if !stopped.status.Exited() || stopped.status.ExitStatus() != 128+int(syscall.SIGTERM) ||
		stopped.saved != stoppedText {
		t.Fatalf("status %v, saved %q\n%s", stopped.status, stopped.saved, stopped.output)
	}
}
