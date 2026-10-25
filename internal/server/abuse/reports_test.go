package abuse_test

import (
	"crypto/hpke"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
)

var operatorKey = func() hpke.PrivateKey {
	key, err := abuse.NewKeyPair()
	if err != nil {
		panic(err)
	}
	return key
}()

func newReports(dataDir string) *abuse.Reports {
	return abuse.NewReports(dataDir, operatorKey.PublicKey(), slog.New(slog.DiscardHandler))
}

func spam(id string) *abuse.Report {
	return &abuse.Report{ID: id, Reason: "spam"}
}

func dayFile(dataDir string, day time.Time, extension string) string {
	return filepath.Join(abuse.Directory(dataDir), day.UTC().Format(time.DateOnly)+extension)
}

// TestStoredReportsAreSealedToTheOperator covers D101: the server writes reports it cannot read back.
func TestStoredReportsAreSealedToTheOperator(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(dataDir)
	now := time.Date(2026, 10, 22, 13, 17, 5, 0, time.UTC)
	id, other := core.NewID().String(), core.NewID().String()
	secret := core.NewSecret()
	link := core.FormatLink("https://nylzo.example", core.NewID(), core.KDFNone, secret)
	contact := "abcdefghijklmnopqrstuvwxyz@example.cz"
	report := abuse.Report{ID: id, Reason: "malware at " + link, Contact: contact, GoodFaith: true}
	if err := reports.Record(now, &report); err != nil {
		t.Fatal(err)
	}
	if err := reports.Record(now, spam(other)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dayFile(dataDir, now, abuse.FileExtension))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || len(lines[0]) != len(lines[1]) {
		t.Fatalf("every sealed report must have the same length: %d lines", len(lines))
	}
	for _, readable := range []string{id, other, contact, "malware", "spam", "2026-10-22"} {
		if strings.Contains(string(data), readable) {
			t.Fatalf("a stored report holds %q in the clear", readable)
		}
	}
	opened, err := abuse.Open(operatorKey, lines[0])
	if err != nil || opened.Date != "2026-10-22" || opened.ID != id || !opened.GoodFaith || opened.Contact != contact ||
		strings.Contains(opened.Reason, core.Encode(secret[:])) {
		t.Fatalf(
			"the operator must read the day, the id, the redacted reason, good faith and the contact: %+v %v",
			opened,
			err,
		)
	}
	if plain, openErr := abuse.Open(operatorKey, lines[1]); openErr != nil || plain.Contact != "" || plain.GoodFaith {
		t.Fatalf("a report without a contact must store none: %+v %v", plain, openErr)
	}
	stranger, err := abuse.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = abuse.Open(stranger, lines[0]); err == nil {
		t.Fatal("another private key opened a report")
	}
	dayStart := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	for _, path := range []string{dayFile(dataDir, now, abuse.FileExtension), abuse.Directory(dataDir)} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.ModTime().Equal(dayStart) || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s must be private and carry the start of the day: %v %v", path, info, statErr)
		}
	}
}

func TestReportFilesHaveACap(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(dataDir)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	if err := reports.Record(now, spam(core.NewID().String())); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(dayFile(dataDir, now, abuse.FileExtension), 16<<20); err != nil {
		t.Fatal(err)
	}
	if err := reports.Record(now, spam(core.NewID().String())); !errors.Is(err, abuse.ErrFull) {
		t.Fatalf("a report that would pass 16 MiB: %v", err)
	}
}

func TestOldReportsAreDropped(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(dataDir)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	oldDays := []time.Time{now.AddDate(0, 0, -21), now.AddDate(0, 0, -8), now.AddDate(0, 0, -7)}
	recentDay := now.AddDate(0, 0, -6)
	legacy := filepath.Join(dataDir, "abuse-reports.jsonl")
	write := func() {
		if err := os.MkdirAll(abuse.Directory(dataDir), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, day := range append(oldDays, recentDay) {
			for _, extension := range []string{abuse.FileExtension, ".jsonl"} {
				if err := os.WriteFile(dayFile(dataDir, day, extension), []byte("x\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := os.WriteFile(legacy, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(legacy, oldDays[1], oldDays[1]); err != nil {
			t.Fatal(err)
		}
	}
	check := func(when string) {
		t.Helper()
		for _, extension := range []string{abuse.FileExtension, ".jsonl"} {
			for _, path := range []string{dayFile(dataDir, oldDays[0], extension), dayFile(dataDir, oldDays[2], extension), legacy} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("%s kept %s: %v", when, filepath.Base(path), err)
				}
			}
			if _, err := os.Stat(dayFile(dataDir, recentDay, extension)); err != nil {
				t.Fatalf("%s dropped reports younger than 7 days: %v", when, err)
			}
		}
	}
	write()
	if err := abuse.Prune(dataDir, now); err != nil {
		t.Fatal(err)
	}
	check("a prune")
	write()
	if err := reports.Record(now, spam(core.NewID().String())); err != nil {
		t.Fatal(err)
	}
	check("a new report")
}
