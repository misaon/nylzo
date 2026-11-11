package harden

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
)

const childVariable = "NYLZO_TEST_HARDEN_CHILD"

var childLevels = map[string]Level{"full": Full, "process": ProcessOnly}

type childReport struct {
	Warning string            `json:"warning"`
	Error   string            `json:"error"`
	Facts   map[string]uint64 `json:"facts"`
}

// TestMain turns the test binary into a child that protects itself, reports and waits, so the tests can look at
// a protected process from outside without protecting the test process.
func TestMain(tests *testing.M) {
	if mode := os.Getenv(childVariable); mode != "" {
		os.Exit(runChild(mode))
	}
	tests.Run()
}

func runChild(mode string) int {
	var report childReport
	if level, ok := childLevels[mode]; ok {
		var err error
		if report.Warning, err = Protect(level); err != nil {
			report.Error = err.Error()
		}
	}
	report.Facts = ownFacts()
	if json.NewEncoder(os.Stdout).Encode(report) != nil {
		return 1
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

// startChild runs a child that protects itself at the level of mode ("plain" stays unprotected) and stays alive
// until the test ends.
func startChild(t *testing.T, mode string) (*exec.Cmd, childReport) {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^$")
	child.Env = append(os.Environ(), childVariable+"="+mode)
	child.Stderr = os.Stderr
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		if waitErr := child.Wait(); waitErr != nil {
			t.Errorf("the %s child failed: %v", mode, waitErr)
		}
	})
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil {
		t.Fatalf("the %s child reported nothing: %v", mode, err)
	}
	var report childReport
	if err = json.Unmarshal(line, &report); err != nil {
		t.Fatalf("the %s child reported %q: %v", mode, line, err)
	}
	if report.Error != "" {
		t.Fatalf("the %s child could not protect itself: %s", mode, report.Error)
	}
	return child, report
}
