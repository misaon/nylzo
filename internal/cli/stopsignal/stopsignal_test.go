package stopsignal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"testing"
)

const exitChild = "NYLZO_TEST_STOPSIGNAL_EXIT"

func TestCtrlCOutranksAnotherStopSignal(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		record func(*Pending)
		want   os.Signal
		held   bool
	}{
		{"Ctrl-C after a termination request", func(p *Pending) {
			p.Keep(syscall.SIGTERM)
			p.Hold(os.Interrupt)
		}, os.Interrupt, true},
		{"a termination request after Ctrl-C", func(p *Pending) {
			p.Keep(os.Interrupt)
			p.Keep(syscall.SIGTERM)
		}, os.Interrupt, false},
		{"a hangup before a termination request", func(p *Pending) {
			p.Hold(syscall.SIGHUP)
			p.Hold(syscall.SIGTERM)
		}, syscall.SIGHUP, true},
		{"a termination request kept after it was held", func(p *Pending) {
			p.Hold(syscall.SIGTERM)
			p.Keep(syscall.SIGTERM)
		}, syscall.SIGTERM, true},
	} {
		pending := new(Pending)
		test.record(pending)
		if got, held := pending.Signal(), pending.Held(); got != test.want || held != test.held {
			t.Errorf("%s: kept %v, held %v", test.name, got, held)
		}
	}
	var none *Pending
	none.Keep(os.Interrupt)
}

func TestTheCauseOfAStopNamesTheSignal(t *testing.T) {
	t.Parallel()
	cause := StopError{Signal: syscall.SIGTERM}
	if !errors.Is(cause, context.Canceled) || cause.Error() != "terminated signal received" {
		t.Fatalf("%q does not match context.Canceled", cause)
	}
}

// Exit ends the process by the signal, which a shell sees as a death by it; Windows gets 128 plus its number.
func TestExitEndsTheProcessByTheSignal(t *testing.T) {
	stops := map[string]syscall.Signal{"interrupt": syscall.SIGINT, "terminate": syscall.SIGTERM}
	if name := os.Getenv(exitChild); name != "" {
		Exit(stops[name])
		return
	}
	t.Parallel()
	for name, stop := range stops {
		if runtime.GOOS != "windows" && signal.Ignored(stop) {
			t.Logf("%s: the tests run with %v ignored, which Exit then cannot raise", name, stop)
			continue
		}
		child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExitEndsTheProcessByTheSignal$")
		child.Env = append(os.Environ(), exitChild+"="+name)
		output, err := child.CombinedOutput()
		exit, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("%s: %v\n%s", name, err, output)
		}
		status, _ := exit.Sys().(syscall.WaitStatus)
		if runtime.GOOS == "windows" && exit.ExitCode() != 128+int(stop) ||
			runtime.GOOS != "windows" && (!status.Signaled() || status.Signal() != stop) {
			t.Fatalf("%s: exit %d, status %v\n%s", name, exit.ExitCode(), status, output)
		}
	}
}
