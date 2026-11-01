package abuse_test

import (
	"bytes"
	"crypto/hpke"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
	return abuse.NewReports(dataDir, operatorKey.PublicKey(), slog.New(slog.DiscardHandler), abuse.FilesBytes(0, 3))
}

func spam(id string) *abuse.Report {
	return &abuse.Report{ID: id, Reason: "spam"}
}

func dayFile(dataDir string, day time.Time) string {
	return filepath.Join(abuse.Directory(dataDir), day.UTC().Format(time.DateOnly)+abuse.FileExtension)
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
	data, err := os.ReadFile(dayFile(dataDir, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 2*abuse.SealedLen {
		t.Fatalf("every sealed report must have the same length: %d bytes for two reports", len(data))
	}
	for _, readable := range []string{id, other, contact, "malware", "spam", "2026-10-22"} {
		if strings.Contains(string(data), readable) {
			t.Fatalf("a stored report holds %q in the clear", readable)
		}
	}
	opened, err := abuse.Open(operatorKey, data)
	if err != nil || len(opened) != 2 || opened[0].Date != "2026-10-22" || opened[0].ID != id ||
		!opened[0].GoodFaith || opened[0].Contact != contact ||
		strings.Contains(opened[0].Reason, core.Encode(secret[:])) {
		t.Fatalf(
			"the operator must read the day, the id, the redacted reason, good faith and the contact: %+v %v",
			opened,
			err,
		)
	}
	if opened[1].Contact != "" || opened[1].GoodFaith {
		t.Fatalf("a report without a contact must store none: %+v", opened[1])
	}
	stranger, err := abuse.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = abuse.Open(stranger, data); err == nil {
		t.Fatal("another private key opened a report")
	}
	if _, err = abuse.Open(operatorKey, data[:len(data)-1]); !errors.Is(err, abuse.ErrSealed) {
		t.Fatalf("a partial report: %v", err)
	}
	dayStart := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	for _, path := range []string{dayFile(dataDir, now), abuse.Directory(dataDir)} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.ModTime().Equal(dayStart) || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s must be private and carry the start of the day: %v %v", path, info, statErr)
		}
	}
}

// TestADamagedReportHidesNoOtherReport covers A4-6 and A2: a partial write or a foreign chunk costs only itself.
func TestADamagedReportHidesNoOtherReport(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(dataDir)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	appendRaw := func(data []byte) {
		file, err := os.OpenFile(dayFile(dataDir, now), os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(data); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	ids := []string{core.NewID().String(), core.NewID().String(), core.NewID().String()}
	if err := reports.Record(now, spam(ids[0])); err != nil {
		t.Fatal(err)
	}
	appendRaw(make([]byte, 1000))
	if err := reports.Record(now, spam(ids[1])); err != nil {
		t.Fatal(err)
	}
	appendRaw(bytes.Repeat([]byte{7}, abuse.SealedLen))
	if err := reports.Record(now, spam(ids[2])); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dayFile(dataDir, now))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := abuse.Open(operatorKey, append(data, 1, 2, 3))
	damaged, isDamaged := errors.AsType[*abuse.DamagedError](err)
	if len(opened) != 3 || opened[0].ID != ids[0] || opened[1].ID != ids[1] || opened[2].ID != ids[2] ||
		!isDamaged || !slices.Equal(damaged.Unreadable, []int{3}) || damaged.TailBytes != 3 {
		t.Fatalf("every whole report must open and the damage must be named: %+v %v", opened, err)
	}
}

func TestTheLongestReportFitsItsSealedLength(t *testing.T) {
	t.Parallel()
	contact := strings.Repeat(`"`, 120) + strings.Repeat("&", 122) + "@example.org"
	reason := strings.Repeat("<", 2000)
	sealed, err := abuse.Seal(operatorKey.PublicKey(), time.Now(), &abuse.Report{
		ID:        core.NewID().String(),
		Reason:    reason,
		Contact:   contact,
		GoodFaith: true,
	})
	if err != nil || len(sealed) != abuse.SealedLen {
		t.Fatalf("the longest allowed report must fit: %d %v", len(sealed), err)
	}
	opened, err := abuse.Open(operatorKey, sealed)
	if err != nil || opened[0].Contact != contact {
		t.Fatalf("the contact must come back as given: %+v %v", opened, err)
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
	if err := os.Truncate(dayFile(dataDir, now), 64<<20); err != nil {
		t.Fatal(err)
	}
	if err := reports.Record(now, spam(core.NewID().String())); !errors.Is(err, abuse.ErrFull) {
		t.Fatalf("a report that would pass 64 MiB: %v", err)
	}
	if room := abuse.FilesBytes(2000, 3); room < 3*2000*8*abuse.SealedLen {
		t.Fatalf("F6: the files must hold every report that 8 days of capped creates allow, got %d bytes", room)
	}
}

func TestOldReportsAreDropped(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(dataDir)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	oldDays := []time.Time{now.AddDate(0, 0, -21), now.AddDate(0, 0, -8), now.AddDate(0, 0, -7)}
	recentDay := now.AddDate(0, 0, -6)
	write := func() {
		if err := os.MkdirAll(abuse.Directory(dataDir), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, day := range append(oldDays, recentDay) {
			if err := os.WriteFile(dayFile(dataDir, day), []byte("x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(when string) {
		t.Helper()
		for _, path := range []string{dayFile(dataDir, oldDays[0]), dayFile(dataDir, oldDays[2])} {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s kept %s: %v", when, filepath.Base(path), err)
			}
		}
		if _, err := os.Stat(dayFile(dataDir, recentDay)); err != nil {
			t.Fatalf("%s dropped reports younger than 7 days: %v", when, err)
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
