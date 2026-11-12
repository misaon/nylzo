package cli

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/stopsignal"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
)

const stoppedText = "saved before the stop"

type stoppedRead struct {
	status syscall.WaitStatus
	saved  string
	output string
}

// A command that a stop signal ends dies by that signal after it returns, so that bash, which waits for it, stops a
// script loop on Ctrl-C. After the claim read first saves the message.
func TestAStopSignalEndsTheProcessByThatSignal(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot end a process by a signal")
	}
	for _, test := range []struct {
		name    string
		stop    syscall.Signal
		atClaim bool
	}{
		{"Ctrl-C before the claim", syscall.SIGINT, false},
		{"termination request before the claim", syscall.SIGTERM, false},
		{"hangup after the claim", syscall.SIGHUP, true},
		{"Ctrl-C after the claim", syscall.SIGINT, true},
		{"termination request after the claim", syscall.SIGTERM, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if signal.Ignored(test.stop) {
				t.Skipf("the tests run with %v ignored, which read then leaves ignored", test.stop)
			}
			stopped := stopRead(t, stopCase{atClaim: test.atClaim, answer: test.atClaim}, test.stop)
			if !stopped.status.Signaled() || stopped.status.Signal() != test.stop {
				t.Fatalf("status %v, want an end by %v\n%s", stopped.status, test.stop, stopped.output)
			}
			saved, reported := "", test.stop.String()+" signal received"
			if test.atClaim {
				saved, reported = stoppedText, ""
			}
			if stopped.saved != saved || !strings.Contains(stopped.output, reported) {
				t.Fatalf("saved %q, want %q and %q\n%s", stopped.saved, saved, reported, stopped.output)
			}
		})
	}
}

// A stop signal ends the process only when the command held it back or failed after it; a command that finished its
// work ends with its code, and so does serve.
func TestTheProcessEndsByAStopSignalOnlyWhenItStoppedTheCommand(t *testing.T) {
	t.Parallel()
	received, interrupted, held := new(stopsignal.Pending), new(stopsignal.Pending), new(stopsignal.Pending)
	received.Keep(syscall.SIGTERM)
	interrupted.Keep(os.Interrupt)
	held.Hold(syscall.SIGTERM)
	for _, test := range []struct {
		rule    stopRule
		code    int
		pending *stopsignal.Pending
		want    os.Signal
	}{
		{endsBySignal, command.ExitError, received, syscall.SIGTERM},
		{endsBySignal, command.ExitOK, received, nil},
		{endsBySignal, command.ExitOK, interrupted, nil},
		{endsBySignal, command.ExitError, interrupted, os.Interrupt},
		{endsBySignal, command.ExitOK, held, syscall.SIGTERM},
		{endsBySignal, command.ExitError, held, syscall.SIGTERM},
		{endsBySignal, command.ExitError, new(stopsignal.Pending), nil},
		{endsWithCode, command.ExitError, held, nil},
	} {
		listed := subcommand{stop: test.rule}
		if got := listed.endingSignal(test.code, test.pending); got != test.want {
			t.Errorf("rule %d, exit %d, held %v: ends by %v, want %v",
				test.rule, test.code, test.pending.Held(), got, test.want)
		}
	}
	for _, listed := range commands() {
		if serve := listed.name == "serve"; serve != (listed.stop == endsWithCode) {
			t.Errorf("%s: stop rule %d; only serve keeps its exit code", listed.name, listed.stop)
		}
	}
}

// A shell starts a background job of a script with Ctrl-C ignored; read leaves it ignored, so it neither warns nor
// stops when the user presses Ctrl-C for the script.
func TestAJobThatStartedIgnoringCtrlCKeepsIgnoringIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not pass an ignored signal to a new process")
	}
	t.Parallel()
	ignoringCtrlC := func(process *exec.Cmd) {
		process.Args = append([]string{"/bin/sh", "-c", `trap "" INT; exec "$0" "$@"`}, process.Args...)
		process.Path = "/bin/sh"
	}
	for _, atClaim := range []bool{false, true} {
		stopped := stopRead(
			t,
			stopCase{atClaim: atClaim, answer: true, prepare: ignoringCtrlC},
			os.Interrupt,
			os.Interrupt,
		)
		if !stopped.status.Exited() || stopped.status.ExitStatus() != 0 || stopped.saved != stoppedText ||
			strings.Contains(stopped.output, "Ctrl-C") {
			t.Fatalf("at the claim %v: status %v, saved %q\n%s", atClaim, stopped.status, stopped.saved, stopped.output)
		}
	}
}

// stopCase says which request of read the server holds, its first or the claim, whether it answers the request after
// the signals, and how the child process starts.
type stopCase struct {
	atClaim bool
	answer  bool
	prepare func(*exec.Cmd)
}

// stopRead runs read in a child process and sends it signals while the server holds one of its requests.
func stopRead(t *testing.T, stop stopCase, signals ...os.Signal) stoppedRead {
	t.Helper()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, stoppedText)
	var taken atomic.Bool
	arrived := make(chan struct{})
	instance.hold = func(r *http.Request) bool {
		if stop.atClaim && !strings.HasSuffix(r.URL.Path, api.ClaimAction) || !taken.CompareAndSwap(false, true) {
			return false
		}
		close(arrived)
		return true
	}
	release := sync.OnceFunc(func() { close(instance.release) })
	defer release()
	out := t.TempDir()
	process := exec.CommandContext(t.Context(), os.Args[0], "read", "--out", out)
	process.Env = append(os.Environ(), reexecEnv+"=1")
	process.Stdin = strings.NewReader(link)
	var output bytes.Buffer
	process.Stdout, process.Stderr = &output, &output
	if stop.prepare != nil {
		stop.prepare(process)
	}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		process.Process.Kill()
		t.Fatal("read sent no request")
	}
	for _, sent := range signals {
		if err := process.Process.Signal(sent); err != nil {
			t.Fatal(err)
		}
	}
	if stop.answer {
		release()
	}
	stopped := stoppedRead{}
	if exit, ok := errors.AsType[*exec.ExitError](process.Wait()); ok {
		stopped.status, _ = exit.Sys().(syscall.WaitStatus)
	}
	stopped.output = output.String()
	if saved, _ := filepath.Glob(filepath.Join(out, "nylzo-*", "message.txt")); len(saved) == 1 {
		data, err := os.ReadFile(saved[0])
		if err != nil {
			t.Fatal(err)
		}
		stopped.saved = string(data)
	}
	return stopped
}

// A termination request is how serve normally ends, so it exits with 0. The Hetzner unit runs serve as the init
// process of a container, where an end by the signal would read as a failure.
func TestServeEndsNormallyOnATerminationRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test signals a child process")
	}
	t.Parallel()
	for _, stop := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		serveUntil(t, stop)
	}
}

func serveUntil(t *testing.T, stop os.Signal) {
	t.Helper()
	pepperFile := filepath.Join(t.TempDir(), "pepper")
	if err := os.WriteFile(pepperFile, []byte(strings.Repeat("0123456789abcdef", 4)), 0o400); err != nil {
		t.Fatal(err)
	}
	process := exec.CommandContext(t.Context(), os.Args[0], "serve")
	process.Env = append(
		os.Environ(),
		reexecEnv+"=1",
		"NYLZO_DATA_DIR="+t.TempDir(),
		"NYLZO_EPOCH_DIR="+t.TempDir(),
		"NYLZO_PEPPER_FILE="+pepperFile,
		"NYLZO_REPORT_KEY="+core.Encode(testReportKey.PublicKey().Bytes()),
		"NYLZO_LISTEN=127.0.0.1:0",
		"NYLZO_ALLOW_SWAP=1",
		"NYLZO_ALLOW_UNERASABLE_STORAGE=1",
		"NYLZO_ALLOW_UNERASED_MEMORY=1",
	)
	logs, err := process.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	lines := bufio.NewScanner(logs)
	for lines.Scan() && !strings.Contains(lines.Text(), `"msg":"listening"`) {
		log.WriteString(lines.Text() + "\n")
	}
	if err = process.Process.Signal(stop); err != nil {
		t.Fatalf("serve did not start: %v\n%s", err, log.String())
	}
	_, _ = io.Copy(io.Discard, logs)
	if err = process.Wait(); err != nil {
		t.Fatalf("serve ended with %v after %v\n%s", err, stop, log.String())
	}
}
