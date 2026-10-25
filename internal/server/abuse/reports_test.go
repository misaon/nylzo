package abuse_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func newReports(t *testing.T, dataDir string) *abuse.Reports {
	t.Helper()
	storage, err := sqlite.Open(t.Context(), filepath.Join(dataDir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	return abuse.NewReports(dataDir, storage, slog.New(slog.DiscardHandler))
}

func spam(id string) *abuse.Report {
	return &abuse.Report{ID: id, Reason: "spam"}
}

func dayFile(dataDir string, day time.Time) string {
	return filepath.Join(abuse.Directory(dataDir), day.UTC().Format(time.DateOnly)+".jsonl")
}

func TestStoredReportsKeepOnlyTheDayAndStayPrivate(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(t, dataDir)
	now := time.Date(2026, 10, 22, 13, 17, 5, 0, time.UTC)
	id, other := core.NewID().String(), core.NewID().String()
	secret := core.NewSecret()
	link := core.FormatLink("https://nylzo.example", core.NewID(), core.KDFNone, secret)
	contact := "abcdefghijklmnopqrstuvwxyz@example.cz"
	report := abuse.Report{ID: id, Reason: "malware at " + link, Contact: contact, GoodFaith: true}
	if err := reports.Record(t.Context(), now, &report); err != nil {
		t.Fatal(err)
	}
	if err := reports.Record(t.Context(), now, spam(other)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dayFile(dataDir, now))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var withContact, withoutContact map[string]any
	if len(lines) != 2 || json.Unmarshal([]byte(lines[0]), &withContact) != nil ||
		json.Unmarshal([]byte(lines[1]), &withoutContact) != nil {
		t.Fatalf("stored reports: %s", data)
	}
	reason, _ := withContact["reason"].(string)
	if len(withContact) != 5 || withContact["date"] != "2026-10-22" || withContact["id"] != id ||
		strings.Contains(reason, core.Encode(secret[:])) || withContact["good_faith"] != true ||
		withContact["contact"] != contact {
		t.Fatalf(
			"a report must store the day, the id, the redacted reason, good faith and the contact as given: %s",
			lines[0],
		)
	}
	if len(withoutContact) != 4 || withoutContact["id"] != other || withoutContact["good_faith"] != false {
		t.Fatalf("a report without a contact must store none: %s", lines[1])
	}
	dayStart := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	for _, path := range []string{dayFile(dataDir, now), abuse.Directory(dataDir)} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.ModTime().Equal(dayStart) || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s must be private and carry the start of the day: %v %v", path, info, statErr)
		}
	}
}

func TestReportsAreCappedPerMessageAndDay(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(t, dataDir)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	reported, full := core.NewID().String(), core.NewID().String()
	for range 3 {
		if err := reports.Record(t.Context(), now, spam(reported)); err != nil {
			t.Fatal(err)
		}
	}
	if err := reports.Record(t.Context(), now, spam(reported)); !errors.Is(err, abuse.ErrLimit) {
		t.Fatalf("the fourth report of a day must report the limit: %v", err)
	}
	stored := func(day time.Time) int {
		data, _ := os.ReadFile(dayFile(dataDir, day))
		return strings.Count(string(data), "\n")
	}
	if count := stored(now); count != 3 {
		t.Fatalf("one message stored %d reports in a day, want 3", count)
	}
	if err := os.Truncate(dayFile(dataDir, now), 16<<20); err != nil {
		t.Fatal(err)
	}
	if err := reports.Record(t.Context(), now, spam(reported)); !errors.Is(err, abuse.ErrLimit) {
		t.Fatalf("an over-budget report needs no room: %v", err)
	}
	if err := reports.Record(t.Context(), now, spam(full)); !errors.Is(err, abuse.ErrFull) {
		t.Fatalf("a report that would pass 16 MiB: %v", err)
	}
	if err := os.Truncate(dayFile(dataDir, now), 0); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := reports.Record(t.Context(), now, spam(full)); err != nil {
			t.Fatal(err)
		}
	}
	if count := stored(now); count != 3 {
		t.Fatalf("a report refused for full files used up the message budget: %d", count)
	}
	tomorrow := now.Add(24 * time.Hour)
	if err := reports.Record(t.Context(), tomorrow, spam(reported)); err != nil || stored(tomorrow) != 1 {
		t.Fatalf("the budget must reset the next UTC day: %v", err)
	}
}

func TestACanceledReportKeepsItsSlot(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(t, dataDir)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	now := time.Date(2027, 1, 15, 10, 0, 0, 0, time.UTC)
	if err := reports.Record(ctx, now, spam(core.NewID().String())); err != nil {
		t.Fatalf("a report whose slot was reserved must be stored even if the client left: %v", err)
	}
	if data, err := os.ReadFile(dayFile(dataDir, now)); err != nil || len(data) == 0 {
		t.Fatalf("report not stored: %v", err)
	}
}

func TestOldReportsAreDropped(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	reports := newReports(t, dataDir)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	oldDays := []time.Time{now.AddDate(0, 0, -21), now.AddDate(0, 0, -8), now.AddDate(0, 0, -7)}
	recentDay := now.AddDate(0, 0, -6)
	legacy := filepath.Join(dataDir, "abuse-reports.jsonl")
	write := func() {
		if err := os.MkdirAll(abuse.Directory(dataDir), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, day := range append(oldDays, recentDay) {
			if err := os.WriteFile(dayFile(dataDir, day), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
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
		for _, path := range []string{dayFile(dataDir, oldDays[0]), dayFile(dataDir, oldDays[2]), legacy} {
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
	if err := reports.Record(t.Context(), now, spam(core.NewID().String())); err != nil {
		t.Fatal(err)
	}
	check("a new report")
}
