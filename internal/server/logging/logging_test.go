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
	"slices"
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
	for name, testCase := range map[string]struct {
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
		got := logging.ErrorText(testCase.err)
		leaks := strings.Contains(got, "/") || strings.Contains(got, id.String()) ||
			strings.Contains(got, fileName) || strings.Contains(got, hex.EncodeToString(id[:]))
		if got != testCase.want || leaks {
			t.Errorf("%s: %q, want %q", name, got, testCase.want)
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

func TestEventsWriteOnlyCountsAndTheFirstErrorWhenFlushed(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	events := logging.NewEvents(slog.New(slog.NewJSONHandler(&buffer, nil)))
	logger, ctx := events.Logger(), t.Context()
	logger.ErrorContext(ctx, "finish-ending", "err", "store: busy", "bytes", 512)
	logger.ErrorContext(ctx, "finish-ending", "err", "store: locked")
	pathErr := &fs.PathError{Op: "remove", Path: "/data/blobs/0a", Err: syscall.EROFS}
	logger.ErrorContext(ctx, "lease-timer", "err", pathErr)
	logger.With("category", "X-Real-IP").WarnContext(ctx, "forwarded-header-ignored")
	logger.WarnContext(ctx, "abuse-files-half-full", "bytes", 1234, "limit", 4096)
	if buffer.Len() != 0 {
		t.Fatalf("an event was written before the flush: %s", buffer.String())
	}
	events.Flush(t.Context())
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSpace(buffer.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		delete(entry, "time")
		lines = append(lines, fmt.Sprint(entry))
	}
	want := []string{
		"map[count:1 level:WARN msg:abuse-files-half-full]",
		"map[count:2 err:store: busy level:ERROR msg:finish-ending]",
		"map[category:X-Real-IP count:1 level:WARN msg:forwarded-header-ignored]",
		"map[count:1 err:remove: " + syscall.EROFS.Error() + " level:ERROR msg:lease-timer]",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("held lines:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	buffer.Reset()
	events.Flush(t.Context())
	if buffer.Len() != 0 {
		t.Fatalf("a flush wrote the events of the window before: %s", buffer.String())
	}
}

func TestHTTPErrorLogHoldsCategoriesOnly(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	events := logging.NewEvents(slog.New(slog.NewJSONHandler(&buffer, nil)))
	errorLog := logging.NewHTTPErrorLog(events)
	errorLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
	for range 40 {
		errorLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	}
	errorLog.Printf("http2: panic serving 203.0.113.99:4242: boom")
	if buffer.Len() != 0 {
		t.Fatalf("a net/http error was written before the flush: %s", buffer.String())
	}
	events.Flush(t.Context())
	logs := buffer.String()
	for _, want := range []string{
		`"level":"WARN","msg":"net/http error","category":"http2","count":40}`,
		`"level":"ERROR","msg":"net/http error","category":"panic","count":1}`,
		`"level":"WARN","msg":"net/http error","category":"tls-handshake","count":1}`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("%s missing: %s", want, logs)
		}
	}
	if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, "boom") {
		t.Fatal("net/http error text reached the log")
	}
}

func TestWindowEndsAtTheStartOfAnHourOnceItSpansAnHour(t *testing.T) {
	t.Parallel()
	at := func(hour, minute, second int) time.Time {
		return time.Date(2026, 11, 7, hour, minute, second, 0, time.UTC)
	}
	for _, testCase := range []struct {
		name  string
		start time.Time
		ticks []time.Time
		ended []bool
	}{
		{
			name:  "started late in an hour",
			start: at(14, 58, 30),
			ticks: []time.Time{at(15, 0, 0), at(15, 59, 0), at(16, 0, 0), at(16, 1, 0), at(17, 0, 0)},
			ended: []bool{false, false, true, false, true},
		},
		{
			name:  "started within the first minute of an hour",
			start: at(15, 0, 40),
			ticks: []time.Time{at(15, 59, 0), at(16, 0, 1)},
			ended: []bool{false, true},
		},
		{
			name:  "a late tick",
			start: at(16, 0, 0),
			ticks: []time.Time{at(17, 23, 0), at(18, 0, 0), at(19, 0, 0)},
			ended: []bool{true, false, true},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			window := logging.StartWindow(testCase.start)
			for i, tick := range testCase.ticks {
				if got := window.Ended(tick); got != testCase.ended[i] {
					t.Fatalf("at %s: ended %t, want %t", tick.Format(time.TimeOnly), got, testCase.ended[i])
				}
			}
		})
	}
}
