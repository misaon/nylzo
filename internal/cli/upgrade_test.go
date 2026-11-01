package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/operator"
	"github.com/misaon/nylzo/internal/core"
)

// The fixture is the data directory that nylzo v0.2.3 serve left after four creates (text, passphrase, attachment,
// and two opens of which one was read) and a kill -9, so its log still holds a frame (schema 5).
const releasedFixture = "testdata/v0.2.3-data.tar.gz"

func TestServeUpgradesTheDataDirectoryOfARelease(t *testing.T) {
	directory := unpackFixture(t, releasedFixture)
	released := releasedRemnants(t, directory)
	address := freeAddress(t)
	pepper := filepath.Join(t.TempDir(), "pepper")
	if err := os.WriteFile(pepper, []byte(strings.Repeat("0123456789abcdef", 4)), 0o400); err != nil {
		t.Fatal(err)
	}
	variables := map[string]string{
		"NYLZO_DATA_DIR":    directory,
		"NYLZO_PEPPER_FILE": pepper,
		"NYLZO_REPORT_KEY":  core.Encode(testReportKey.PublicKey().Bytes()),
		"NYLZO_LISTEN":      address,
		"NYLZO_ALLOW_SWAP":  "1",
	}
	if runtime.GOOS != "linux" {
		variables["NYLZO_ALLOW_UNERASABLE_STORAGE"] = "1"
	}
	ctx, stop := context.WithCancel(t.Context())
	var serveLog bytes.Buffer
	served := make(chan int, 1)
	go func() {
		served <- operator.RunServe(ctx, &command.Environment{
			Stdin:    strings.NewReader(""),
			Stdout:   io.Discard,
			Stderr:   &serveLog,
			Getenv:   getenvFrom(variables),
			Terminal: noTerminal{},
			Version:  "test",
		}, nil)
	}()
	instance := "http://" + address
	if !healthy(instance) {
		stop()
		code := <-served
		t.Fatalf("serve did not start on a schema 5 data directory: exit %d, %s", code, serveLog.String())
	}
	link, _ := createText(t, &testServer{url: instance}, "after the upgrade")
	read := runWith(t, link, nil, nil, "read", "--stdout")
	stop()
	if code := <-served; code != command.ExitOK {
		t.Fatalf("serve: exit %d, %s", code, serveLog.String())
	}
	if read.code != command.ExitOK || read.stdout != "after the upgrade\n" {
		t.Fatalf("a message created after the upgrade: exit %d %q %s", read.code, read.stdout, read.stderr)
	}
	if !strings.Contains(serveLog.String(), `"msg":"store-upgraded"`) {
		t.Fatalf("the upgrade logged no notice: %s", serveLog.String())
	}
	for _, name := range released.blobNames {
		if _, err := os.Stat(filepath.Join(directory, "blobs", name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the blob %s of a released message is still there: %v", name, err)
		}
	}
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		content, readErr := os.ReadFile(path)
		for _, remnant := range released.remnants {
			if bytes.Contains(content, remnant) {
				t.Errorf("%s still holds bytes of a message from before the upgrade", path)
				break
			}
		}
		return readErr
	})
	if err != nil {
		t.Fatal(err)
	}
}

func unpackFixture(t *testing.T, name string) string {
	t.Helper()
	compressed, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	unzipped, err := gzip.NewReader(compressed)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "data")
	if err = os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := tar.NewReader(unzipped)
	for {
		header, nextErr := archive.Next()
		if errors.Is(nextErr, io.EOF) {
			return directory
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		target := filepath.Join(directory, filepath.FromSlash(header.Name))
		if header.Typeflag == tar.TypeDir {
			err = os.MkdirAll(target, 0o700)
		} else {
			var content []byte
			if content, err = io.ReadAll(archive); err == nil {
				err = os.WriteFile(target, content, 0o600)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

type remnants struct {
	blobNames []string
	remnants  [][]byte
}

// releasedRemnants reads, without writing the database, the bytes that only the released messages hold.
func releasedRemnants(t *testing.T, directory string) remnants {
	t.Helper()
	var blobNames []string
	var found [][]byte
	entries, err := os.ReadDir(filepath.Join(directory, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(directory, "blobs", entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		blobNames = append(blobNames, entry.Name())
		middle := len(content) / 2
		found = append(found, content[:32], content[middle:middle+32], content[len(content)-32:])
	}
	database, err := sql.Open("sqlite", "file:"+filepath.Join(directory, "nylzo.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.Query(`SELECT locator, read_wrap, manage_wrap, record FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var locator, readWrap, manageWrap, record []byte
		if err = rows.Scan(&locator, &readWrap, &manageWrap, &record); err != nil {
			t.Fatal(err)
		}
		found = append(found, locator, readWrap[:32], manageWrap[:32], record[:32])
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(blobNames) != 4 || len(found) != 4*3+4*4 {
		t.Fatalf("the fixture holds %d blobs and %d remnants, want 4 messages", len(blobNames), len(found))
	}
	return remnants{blobNames: blobNames, remnants: found}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func healthy(instance string) bool {
	for range 200 {
		if response, err := http.Get(instance + "/healthz"); err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
