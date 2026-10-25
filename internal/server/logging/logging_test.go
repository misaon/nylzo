package logging_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/logging"
)

func TestErrorTextDropsPathsAndIDs(t *testing.T) {
	t.Parallel()
	id := core.NewID()
	fileName := hex.EncodeToString([]byte(id.String()))
	hexPath := "/data/blobs/" + fileName
	for name, c := range map[string]struct {
		err  error
		want string
	}{
		"path error": {
			fmt.Errorf("delete: %w", &fs.PathError{Op: "remove", Path: hexPath, Err: syscall.EROFS}),
			"delete: remove: " + syscall.EROFS.Error(),
		},
		"link error": {
			&os.LinkError{Op: "link", Old: hexPath + ".tmp", New: hexPath, Err: syscall.EEXIST},
			"link: " + syscall.EEXIST.Error(),
		},
		"joined": {
			fmt.Errorf("sweep: %w", errors.Join(
				errors.New("sqlite: busy"),
				&fs.PathError{Op: "unlink", Path: "/data/abuse/2026-10-01.jsonl", Err: syscall.EACCES},
			)),
			"sweep: sqlite: busy\nunlink: " + syscall.EACCES.Error(),
		},
		"id in text":    {errors.New("store: row " + id.String() + " failed"), "store: row [redacted] failed"},
		"hex in text":   {fmt.Errorf("store: ref %x failed", id[:]), "store: ref [redacted] failed"},
		"file name":     {errors.New("disk: " + fileName + " is busy"), "disk: [redacted] is busy"},
		"plain message": {errors.New("sqlite: database is locked"), "sqlite: database is locked"},
	} {
		got := logging.ErrorText(c.err)
		leaks := strings.Contains(got, "/") || strings.Contains(got, id.String()) ||
			strings.Contains(got, fileName) || strings.Contains(got, hex.EncodeToString(id[:]))
		if got != c.want || leaks {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestLogLinesCarryTheMinuteOnly(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	nested := slog.Group("g", slog.Time("time", time.Date(2026, 10, 23, 10, 17, 42, 5, time.UTC)))
	logging.New(&buffer).InfoContext(t.Context(), "event", nested)
	var line struct {
		Time  time.Time `json:"time"`
		Group struct {
			Time time.Time `json:"time"`
		} `json:"g"`
	}
	if err := json.Unmarshal(buffer.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line.Time.IsZero() || !line.Time.Equal(line.Time.Truncate(time.Minute)) || line.Time.Location() != time.UTC {
		t.Fatalf("a log line carries a time finer than the minute: %s", buffer.String())
	}
	if line.Group.Time.Second() != 42 {
		t.Fatalf("a nested attribute named time was changed: %s", buffer.String())
	}
}

func TestHTTPErrorLogKeepsCategoriesOnlyAndLimitsThem(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	errorLog := logging.NewHTTPErrorLog(slog.New(slog.NewJSONHandler(&buffer, nil)), func() time.Time { return now })
	errorLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
	for range 40 {
		errorLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	}
	errorLog.Printf("http2: panic serving 203.0.113.99:4242: boom")
	logs := buffer.String()
	for _, want := range []string{`"category":"tls-handshake"`, `"category":"http2"`, `"category":"panic"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("%s missing: %s", want, logs)
		}
	}
	if n := strings.Count(logs, `"level":"WARN","msg":"net/http error"`); n != 20 {
		t.Fatalf("net/http warnings must be limited to a burst of 20, got %d", n)
	}
	if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, "boom") {
		t.Fatal("net/http error text reached the log")
	}
	for _, dropped := range []string{`"dropped":21`, `"dropped":0`} {
		now = now.Add(time.Second)
		buffer.Reset()
		errorLog.Printf("http2: server: error reading preface")
		if !strings.Contains(buffer.String(), dropped) {
			t.Fatalf("dropped lines must be reported once and then reset, want %s: %s", dropped, buffer.String())
		}
	}
}
