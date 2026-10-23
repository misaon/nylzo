package server

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
		"path error":    {fmt.Errorf("delete: %w", &fs.PathError{Op: "remove", Path: hexPath, Err: syscall.EROFS}), "delete: remove: " + syscall.EROFS.Error()},
		"link error":    {&os.LinkError{Op: "link", Old: hexPath + ".tmp", New: hexPath, Err: syscall.EEXIST}, "link: " + syscall.EEXIST.Error()},
		"joined":        {fmt.Errorf("sweep: %w", errors.Join(errors.New("sqlite: busy"), &fs.PathError{Op: "unlink", Path: "/data/abuse/2026-10-01.jsonl", Err: syscall.EACCES})), "sweep: sqlite: busy\nunlink: " + syscall.EACCES.Error()},
		"id in text":    {errors.New("store: row " + id.String() + " failed"), "store: row [redacted] failed"},
		"hex in text":   {fmt.Errorf("store: ref %x failed", id[:]), "store: ref [redacted] failed"},
		"file name":     {errors.New("disk: " + fileName + " is busy"), "disk: [redacted] is busy"},
		"plain message": {errors.New("sqlite: database is locked"), "sqlite: database is locked"},
	} {
		got := ErrorText(c.err)
		if got != c.want || strings.Contains(got, "/") || strings.Contains(got, id.String()) || strings.Contains(got, fileName) || strings.Contains(got, fmt.Sprintf("%x", id[:])) {
			t.Fatalf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestSpentChallengesStayBounded(t *testing.T) {
	t.Parallel()
	c := newSpentChallenges()
	c.max = 64
	for i := range c.max {
		c.seen([challengeNonceLen]byte{byte(i)}, 100, 100)
	}
	if !c.seen([challengeNonceLen]byte{1}, 100, 100) {
		t.Fatal("a remembered nonce was not reported as seen")
	}
	c.seen([challengeNonceLen]byte{0xff, 1}, 1000, 1000)
	if len(c.expires) != 1 {
		t.Fatalf("expired nonces were kept: %d", len(c.expires))
	}
	for i := range 4 * c.max {
		c.seen([challengeNonceLen]byte{byte(i), byte(i >> 8), 2}, 2000, 1000)
	}
	if len(c.expires) > c.max {
		t.Fatalf("the nonce memory grew past its bound: %d", len(c.expires))
	}
}

func TestLogLinesCarryTheMinuteOnly(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	NewLogger(&buf).Info("event", slog.Group("g", slog.Time("time", time.Date(2026, 10, 23, 10, 17, 42, 5, time.UTC))))
	var line struct {
		Time  time.Time `json:"time"`
		Group struct {
			Time time.Time `json:"time"`
		} `json:"g"`
	}
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line.Time.IsZero() || !line.Time.Equal(line.Time.Truncate(time.Minute)) || line.Time.Location() != time.UTC {
		t.Fatalf("a log line carries a time finer than the minute: %s", buf.String())
	}
	if line.Group.Time.Second() != 42 {
		t.Fatalf("a nested attribute named time was changed: %s", buf.String())
	}
}

func TestTheDefaultLoggerCarriesTheMinuteOnly(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = w
	cfg := Defaults()
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	s, err := New(cfg, nil, nil)
	os.Stderr = stderr
	if err != nil {
		t.Fatal(err)
	}
	s.log.Info("event")
	w.Close()
	var line struct {
		Time time.Time `json:"time"`
	}
	dec := json.NewDecoder(r)
	for dec.More() {
		if err := dec.Decode(&line); err != nil {
			t.Fatal(err)
		}
		if line.Time.IsZero() || !line.Time.Equal(line.Time.Truncate(time.Minute)) {
			t.Fatalf("the default logger writes a time finer than the minute: %v", line.Time)
		}
	}
}
