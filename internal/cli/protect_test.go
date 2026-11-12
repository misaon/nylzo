package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/harden"
)

type watchedInput struct{ read bool }

func (w *watchedInput) Read([]byte) (int, error) {
	w.read = true
	return 0, io.EOF
}

func noHarden(harden.Level) (string, error) { return "", nil }

func protectedRun(
	stdin io.Reader,
	variables map[string]string,
	hook func(harden.Level) (string, error),
	arguments ...string,
) commandResult {
	var stdout, stderr bytes.Buffer
	code := Run(arguments, &command.Environment{
		Stdin:    stdin,
		Stdout:   &stdout,
		Stderr:   &stderr,
		Getenv:   getenvFrom(variables),
		Terminal: noTerminal{},
		Version:  "test",
		Harden:   hook,
	}).Code
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// TestEveryCommandProtectsItsProcessFirst covers F07: each command that can hold a secret hardens its process
// before it parses a flag or reads anything, and a new command needs a decision here.
func TestEveryCommandProtectsItsProcessFirst(t *testing.T) {
	want := map[string]harden.Level{
		"create": harden.Full, "read": harden.Full, "open": harden.Full, "web": harden.Full,
		"status": harden.Full, "cancel": harden.Full, "report": harden.Full, "burn": harden.Full,
		"report-key": harden.Full, "reports": harden.Full, "sweep-once": harden.Full, "store-check": harden.Full,
		"takedown": harden.Full, "serve": harden.ProcessOnly,
		"verify-client": harden.Off, "version": harden.Off,
	}
	for _, listed := range commands() {
		t.Run(listed.name, func(t *testing.T) {
			level, ok := want[listed.name]
			if !ok {
				t.Fatal("decide how this command protects its process")
			}
			var applied []harden.Level
			result := protectedRun(&watchedInput{}, nil, func(level harden.Level) (string, error) {
				applied = append(applied, level)
				return "", nil
			}, listed.name, "-h")
			if result.code != command.ExitOK {
				t.Fatalf("%s -h: exit %d, %s", listed.name, result.code, result.stderr)
			}
			if level == harden.Off && len(applied) != 0 {
				t.Fatalf("%s holds no secret, yet hardened its process at %v", listed.name, applied)
			}
			if level != harden.Off && (len(applied) != 1 || applied[0] != level) {
				t.Fatalf("%s hardened its process at %v, want [%v]", listed.name, applied, level)
			}
		})
	}
}

func TestACommandReadsNoSecretWithoutProtection(t *testing.T) {
	failed := func(harden.Level) (string, error) { return "", errors.New("prctl: operation not permitted") }
	for _, listed := range commands() {
		if listed.protect == harden.Off {
			continue
		}
		for name, hook := range map[string]func(harden.Level) (string, error){"failed": failed, "missing": nil} {
			t.Run(listed.name+"/"+name, func(t *testing.T) {
				stdin := &watchedInput{}
				result := protectedRun(stdin, nil, hook, listed.name, "--server", "https://nylzo.example")
				if result.code != command.ExitError || stdin.read || result.stdout != "" {
					t.Fatalf("exit %d, stdin read %t, stdout %q", result.code, stdin.read, result.stdout)
				}
				if !strings.Contains(result.stderr, "protect") {
					t.Fatalf("stderr does not say why: %q", result.stderr)
				}
			})
		}
	}
	if result := protectedRun(&watchedInput{}, nil, nil, "version"); result.code != command.ExitOK {
		t.Fatalf("version needs no protection: exit %d, %s", result.code, result.stderr)
	}
}

// TestACommandRefusesSettingsThatLogRequestHeaders covers F11: GODEBUG=http2debug makes net/http print every header,
// verifier and signature included, and the refusal comes before stdin is read.
func TestACommandRefusesSettingsThatLogRequestHeaders(t *testing.T) {
	for _, listed := range commands() {
		if listed.protect == harden.Off {
			continue
		}
		t.Run(listed.name, func(t *testing.T) {
			stdin := &watchedInput{}
			variables := map[string]string{"GODEBUG": "http2debug=2"}
			result := protectedRun(stdin, variables, noHarden, listed.name, "--server", "https://nylzo.example")
			if result.code != command.ExitError || stdin.read || result.stdout != "" ||
				!strings.Contains(result.stderr, "GODEBUG=http2debug=2") {
				t.Fatalf(
					"exit %d, stdin read %t, stdout %q, stderr %q",
					result.code,
					stdin.read,
					result.stdout,
					result.stderr,
				)
			}
		})
	}
	if result := protectedRun(
		&watchedInput{},
		map[string]string{"GODEBUG": "http2debug=2"},
		nil,
		"version",
	); result.code != command.ExitOK {
		t.Fatalf("version sends no request: exit %d, %s", result.code, result.stderr)
	}
}

func TestTheSwapWarningAndItsOptOut(t *testing.T) {
	exposed := func(harden.Level) (string, error) { return "swap on /swap.img is not encrypted", nil }
	shown := protectedRun(&watchedInput{}, nil, exposed, "status", "-h")
	if !strings.HasPrefix(shown.stderr, "nylzo: warning: swap on /swap.img is not encrypted; NYLZO_ALLOW_SWAP=1") {
		t.Fatalf("stderr: %q", shown.stderr)
	}
	hidden := protectedRun(&watchedInput{}, map[string]string{"NYLZO_ALLOW_SWAP": "1"}, exposed, "status", "-h")
	if hidden.code != command.ExitOK || hidden.stderr != "" {
		t.Fatalf("NYLZO_ALLOW_SWAP=1: exit %d, stderr %q", hidden.code, hidden.stderr)
	}
}
