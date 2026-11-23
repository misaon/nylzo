package command

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/misaon/nylzo/internal/privatefile"
	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
)

func TestFailureMessagesAreEscaped(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	Failf(&Environment{Stderr: &stderr}, "%s", "spoof\x1b]0;x\x07\r\u202enylzo: saved")
	if got := stderr.String(); got != `nylzo: spoof\u001b]0;x\u0007\u000d\u202enylzo: saved`+"\n" {
		t.Fatalf("control and format characters must reach stderr escaped: %q", got)
	}
}

func TestReadingAFileNamesTheProblemWithoutThePath(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	long := filepath.Join(directory, "long")
	if err := os.WriteFile(long, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]error{
		filepath.Join(directory, "absent"): errFileMissing,
		directory:                          errFileUnreadable,
		long:                               errTooLong,
	} {
		if data, err := ReadFileLimited(path, 4); !errors.Is(err, want) || data != nil ||
			strings.Contains(err.Error(), directory) {
			t.Errorf("%s: %q, %v, want %v", path, data, err, want)
		}
	}
	if _, err := ReadLimited(iotest.ErrReader(io.ErrUnexpectedEOF), 4); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a failing reader: %v", err)
	}
	denied := &fs.PathError{Op: "open", Path: directory, Err: fs.ErrPermission}
	if err := Unreadable(denied); !errors.Is(err, errFileDenied) {
		t.Errorf("a denied file: %v", err)
	}
}

func TestAPrivateFileThatCannotBeWrittenIsRemoved(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "message.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err = WritePrivateFile(file, "obsah"); err == nil {
		t.Fatal("wrote to a closed file")
	}
	if _, err = os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the unwritten file stays: %v", err)
	}
}

func TestARefusedFileSystemIsToldApartFromAnExistingFile(t *testing.T) {
	t.Parallel()
	if PrivateFileProblem(privatefile.ErrNotPrivate) == PrivateFileProblem(fs.ErrExist) {
		t.Fatal("a file system that cannot keep files private reads like an existing file")
	}
}

func TestTheLocalWebClientIsTheEmbeddedOne(t *testing.T) {
	t.Parallel()
	embedded, err := webclient.Load(web.Files())
	build, localErr := (&Environment{}).LocalWebClient()
	if err == nil && embedded.Built {
		if localErr != nil || !build.Built {
			t.Fatalf("the embedded client is built, yet: %v", localErr)
		}
		return
	}
	if !errors.Is(localErr, ErrNoLocalClient) {
		t.Fatalf("a build without the web client: %v", localErr)
	}
}
