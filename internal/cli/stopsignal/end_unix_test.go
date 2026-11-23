//go:build !windows

package stopsignal

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
)

const endChild = "NYLZO_TEST_STOPSIGNAL_END"

// Ctrl-C ignored from the start cannot end the process, so End exits with the status a shell reports for it.
func TestEndExitsWithTheSignalStatusWhenTheSignalCannotEndTheProcess(t *testing.T) {
	switch os.Getenv(endChild) {
	case "":
	case "interrupt":
		Notify(make(chan os.Signal, 1), os.Interrupt)
		if !signal.Ignored(os.Interrupt) {
			End(nil, 4)
		}
		End(os.Interrupt, 0)
	default:
		End(nil, 3)
	}
	t.Parallel()
	for name, want := range map[string]int{"interrupt": 128 + int(syscall.SIGINT), "code": 3} {
		child := exec.CommandContext(
			t.Context(),
			"/bin/sh",
			"-c",
			`trap "" INT; exec "$0" "$@"`,
			os.Args[0],
			"-test.run=^TestEndExitsWithTheSignalStatusWhenTheSignalCannotEndTheProcess$",
		)
		child.Env = append(os.Environ(), endChild+"="+name)
		if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
			child.Env = append(child.Env, "GOCOVERDIR="+directory.Value.String())
		}
		output, err := child.CombinedOutput()
		if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != want {
			t.Errorf("%s: %v, want exit %d\n%s", name, err, want, output)
		}
	}
}
