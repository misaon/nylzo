package quarantine

import (
	"bytes"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/misaon/nylzo/internal/privatefile"
)

var systemDownloadMark = regexp.MustCompile(`^0081;([0-9a-f]{8});;$`)

func TestMarkWritesTheSystemDownloadMarkWithoutAnAddress(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "attachment.bin")
	file, err := privatefile.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Unix()
	err = Mark(file)
	after := time.Now().Unix()
	if err = errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	value := make([]byte, 128)
	size, err := unix.Getxattr(path, "com.apple.quarantine", value)
	if err != nil {
		t.Fatal(err)
	}
	match := systemDownloadMark.FindSubmatch(value[:size])
	if match == nil {
		t.Fatalf("the mark is %q", value[:size])
	}
	seconds, err := strconv.ParseInt(string(match[1]), 16, 64)
	if err != nil || seconds < before || seconds > after {
		t.Fatalf("the mark's time %q is outside %d..%d", match[1], before, after)
	}
}

func TestMarkKeepsTheMarkOfAQuarantinedProcess(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "attachment.bin")
	file, err := privatefile.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	system := []byte("0083;6af49cfa;Safari;BC4DFC58-0D26-460D-9688-81D119298642")
	if err = unix.Fsetxattr(int(file.Fd()), attribute, system, 0); err != nil {
		t.Fatal(err)
	}
	if err = Mark(file); err != nil {
		t.Fatal(err)
	}
	value := make([]byte, 128)
	size, err := unix.Fgetxattr(int(file.Fd()), attribute, value)
	if err != nil || !bytes.Equal(value[:size], system) {
		t.Fatalf("the system's mark became %q: %v", value[:max(size, 0)], err)
	}
}
