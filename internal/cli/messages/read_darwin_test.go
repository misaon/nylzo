package messages

import (
	"io"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core/envelope"
)

func TestReadMarksTheTextAndTheAttachmentAsDownloaded(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	apiClient, err := client.New(instance.url)
	if err != nil {
		t.Fatal(err)
	}
	text := "with a program attached"
	message := content{
		meta: envelope.Meta{Text: &text, File: &envelope.FileMeta{Name: "setup.command", Size: 4}},
		file: []byte("exit"),
	}
	created, err := createOnce(
		t.Context(),
		&command.Environment{Stderr: io.Discard},
		apiClient,
		&createInput{
			options:   &createOptions{opens: 1},
			message:   &message,
			policy:    envelope.Policy{OpensMax: 1, TTLSeconds: 3600},
			maxObject: envelope.ObjectLimit(envelope.MaxFileBytes),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	paths := readPaths(t, runWith(t, created.link, "read", "--out", t.TempDir()))
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
