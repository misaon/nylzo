//go:build unix

package messages

import (
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
)

const sizeLimitChild = "NYLZO_TEST_SIZE_LIMIT"

func limitFileSize() (func(), error) {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &limit); err != nil {
		return nil, err
	}
	restore := limit
	limit.Cur = 4
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &limit); err != nil {
		return nil, err
	}
	return func() { _ = unix.Setrlimit(unix.RLIMIT_FSIZE, &restore) }, nil
}

// The file size limit plays a full disk in a child process, since it holds for the whole process.
func TestWritesThatTheDiskCutsShortLeaveNoFile(t *testing.T) {
	if scenario := os.Getenv(sizeLimitChild); scenario != "" {
		run := map[string]func(*testing.T) error{
			"read":       readPastTheSizeLimit,
			"passphrase": generatePastTheSizeLimit,
			"manage":     manageTokenPastTheSizeLimit,
		}[scenario]
		if err := run(t); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Parallel()
	arguments := []string{"-test.run=^TestWritesThatTheDiskCutsShortLeaveNoFile$"}
	if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
		arguments = append(arguments, "-test.gocoverdir="+directory.Value.String())
	}
	for _, scenario := range []string{"read", "passphrase", "manage"} {
		child := exec.CommandContext(t.Context(), os.Args[0], arguments...)
		child.Env = append(os.Environ(), sizeLimitChild+"="+scenario)
		if output, err := child.CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", scenario, err, output)
		}
	}
}

func leftFiles(base string) error {
	var left []string
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			left = append(left, path)
		}
		return err
	})
	if err == nil && len(left) > 0 {
		err = errors.New("left " + strings.Join(left, ", "))
	}
	return err
}

func readPastTheSizeLimit(t *testing.T) error {
	t.Helper()
	base := t.TempDir()
	directory, err := outputDirectory(t.Context(), base)
	if err != nil {
		return err
	}
	text := "longer than the limit"
	restore, err := limitFileSize()
	if err != nil {
		return err
	}
	_, err = saveMessage(
		&command.Environment{Stdout: io.Discard, Stderr: io.Discard, Terminal: noTerminal{}},
		directory,
		base,
		false,
		envelope.Payload{Meta: envelope.Meta{Text: &text}},
	)
	restore()
	if !errors.Is(err, errWriteOutput) {
		return errors.New("the save did not fail as a write: " + errorText(err))
	}
	return leftFiles(base)
}

func generatePastTheSizeLimit(t *testing.T) error {
	t.Helper()
	base := t.TempDir()
	restore, err := limitFileSize()
	if err != nil {
		return err
	}
	result := runMessage(t, RunCreate, messageRun{stdin: "text"},
		"--server", "https://nylzo.example", "--generate-passphrase-file", filepath.Join(base, "passphrase"))
	restore()
	if result.code != command.ExitError || !strings.Contains(result.stderr, errPassphraseNotWritten.Error()) {
		return errors.New("the create did not fail at the passphrase: " + result.stderr)
	}
	return leftFiles(base)
}

func manageTokenPastTheSizeLimit(t *testing.T) error {
	t.Helper()
	limited := make(chan func(), 1)
	failed := make(chan error, 1)
	instance := serveThrough(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			next.ServeHTTP(writer, request)
			if request.Method == http.MethodPost && request.URL.Path == api.MessagesPath {
				restore, err := limitFileSize()
				if err != nil {
					failed <- err
					return
				}
				limited <- restore
			}
		})
	})
	manage := filepath.Join(t.TempDir(), "manage")
	result := runMessage(
		t,
		RunCreate,
		messageRun{stdin: "text"},
		"--server", instance.url, "--manage-token-file", manage,
	)
	select {
	case restore := <-limited:
		restore()
	case err := <-failed:
		return err
	default:
		return errors.New("the server stored no message: " + result.stderr)
	}
	if result.code != command.ExitError || !strings.HasPrefix(result.stdout, instance.url) ||
		!strings.Contains(result.stderr, "its manage token file could not be written") {
		return errors.New("the create did not report the unwritten manage token: " + result.stderr)
	}
	return nil
}

func errorText(err error) string {
	if err == nil {
		return "no error"
	}
	return err.Error()
}
