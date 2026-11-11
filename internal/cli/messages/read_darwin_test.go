package messages

import (
	"strings"
	"testing"

	"golang.org/x/sys/unix"
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
