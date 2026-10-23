package server

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestErrorTextDropsPathsAndIDs(t *testing.T) {
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
