package messages

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core/envelope"
)

func TestReadMarksTheTextAndTheAttachmentAsDownloaded(t *testing.T) {
	t.Parallel()
	link := createWithFile(t, newTestServer(t), "with a program attached", "setup.command", []byte("exit"))
	paths := readPaths(t, runWith(t, link, "read", "--out", t.TempDir()))
	if len(paths) != 2 {
		t.Fatalf("read wrote %v", paths)
	}
	for _, path := range paths {
		value := make([]byte, 128)
		size, getErr := unix.Getxattr(path, "com.apple.quarantine", value)
		if getErr != nil || !strings.HasPrefix(string(value[:size]), "0081;") {
			t.Fatalf("%s carries no download mark: %q %v", path, value[:max(size, 0)], getErr)
		}
	}
}

// An append-only directory lets read write the text but not remove it after the attachment fails.
func TestReadNamesThePlaintextAFailedWriteLeavesBehind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needs a non-root user")
	}
	t.Parallel()
	text := "left behind"
	for _, fallback := range []bool{true, false} {
		base := t.TempDir()
		first := appendOnlyDirectory(t, base, "notes.bin", attachmentPrefix+"notes.bin")
		if !fallback {
			if err := os.Chmod(base, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(base, 0o700) })
		}
		var stderr bytes.Buffer
		paths, err := saveMessage(
			&command.Environment{Stdout: io.Discard, Stderr: &stderr, Terminal: noTerminal{}},
			openRoot(t, first),
			base,
			false,
			notesPayload(&text),
		)
		if saved := err == nil && len(paths) == 2 && filepath.Dir(paths[0]) != first; saved != fallback {
			t.Fatalf("fallback %v: saved %v (%v)", fallback, paths, err)
		}
		left := filepath.Join(first, textFileName)
		if got := mustRead(t, left); got != text || !strings.Contains(stderr.String(), "could not remove "+left) {
			t.Fatalf("fallback %v: %s holds %q, stderr %q", fallback, left, got, stderr.String())
		}
	}
}

const fileSizeLimitChild = "NYLZO_TEST_FILE_SIZE_LIMIT"

// The file size limit cuts the text short, and the append-only directory keeps the part that was written.
func TestReadNamesAPartialFileAFailedWriteLeavesBehind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needs a non-root user")
	}
	if os.Getenv(fileSizeLimitChild) != "" {
		writePastTheFileSizeLimit(t)
		return
	}
	t.Parallel()
	verdict := childVerdict(t, "TestReadNamesAPartialFileAFailedWriteLeavesBehind", fileSizeLimitChild, false)
	if verdict != "named" {
		t.Fatalf("the child reported %q", verdict)
	}
}

func writePastTheFileSizeLimit(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	first := appendOnlyDirectory(t, base)
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	restore := limit
	limit.Cur = 4
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Setrlimit(unix.RLIMIT_FSIZE, &restore) })
	text := "longer than the limit"
	var stderr bytes.Buffer
	_, err := saveMessage(
		&command.Environment{Stdout: io.Discard, Stderr: &stderr, Terminal: noTerminal{}},
		openRoot(t, first),
		base,
		false,
		envelope.Payload{Meta: envelope.Meta{Text: &text}},
	)
	verdict := "named"
	if err == nil || !strings.Contains(stderr.String(), "could not remove "+filepath.Join(first, textFileName)) {
		verdict = fmt.Sprintf("%v, stderr %q", err, stderr.String())
	}
	reportVerdict(t, verdict)
}

// appendOnlyDirectory makes a directory where files can be created but not removed, with the given entries in it.
func appendOnlyDirectory(t *testing.T, base string, entries ...string) string {
	t.Helper()
	directory := filepath.Join(base, "first")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.Mkdir(filepath.Join(directory, entry), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Chflags(directory, unix.UF_APPEND); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Chflags(directory, 0) })
	return directory
}
