package quarantine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/misaon/nylzo/internal/privatefile"
)

func TestMarkPutsAPrivateFileInTheInternetZoneWithoutAnAddress(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "attachment.bin")
	file, err := privatefile.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = Mark(file)
	if err == nil {
		_, err = file.WriteString("content")
	}
	if err = errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	mark, err := os.ReadFile(path + ":Zone.Identifier")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[ZoneTransfer]\r\nZoneId=3\r\nHostUrl=about:internet\r\n"; string(mark) != want {
		t.Fatalf("the mark is %q, want %q", mark, want)
	}
	if content, readErr := os.ReadFile(path); readErr != nil || string(content) != "content" {
		t.Fatalf("the file holds %q: %v", content, readErr)
	}
}
