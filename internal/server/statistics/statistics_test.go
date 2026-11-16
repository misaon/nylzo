package statistics_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/statistics"
)

// A count reaches the disk only when its hour has ended, and a visitor or a country only when its UTC day has, so
// the file never tells the minute of a visit; a chart reads the same totals before and after, and a period past
// its retention is gone.
func TestCountsReachTheDiskWhenTheirPeriodEnds(t *testing.T) {
	store, err := adminstore.Open(t.Context(), filepath.Join(t.TempDir(), adminstore.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	counts := statistics.New(store)
	noon := time.Date(2026, 11, 15, 12, 30, 0, 0, time.UTC)
	counts.Add("views.people", 5, noon)
	if series, _ := counts.Read(t.Context(), adminstore.Hourly, noon.Add(-time.Hour), noon); len(series.Values) != 0 {
		t.Fatalf("a server that does not count kept %v", series.Values)
	}
	counts.Enable(true)
	counts.Add("views.people", 2, noon)
	counts.Add("views.people", 3, noon.Add(time.Hour))
	counts.Add(statistics.MetricVisitors, 2, noon)
	counts.Add(statistics.CountryPrefix+"CZ", 4, noon)
	counts.Add(statistics.CountryPrefix+"DE", 1, noon)
	check := func(stage string) {
		t.Helper()
		hours, readErr := counts.Read(t.Context(), adminstore.Hourly, noon.Add(-time.Hour), noon.Add(2*time.Hour))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got := hours.Values["views.people"]; len(got) != 4 || got[0] != 0 || got[1] != 2 || got[2] != 3 {
			t.Fatalf("%s: hours are %v", stage, got)
		}
		if _, hourly := hours.Values[statistics.MetricVisitors]; hourly ||
			len(hours.Breakdown(statistics.CountryPrefix)) != 0 {
			t.Fatalf("%s: the hours hold a daily-only metric: %v", stage, hours.Values)
		}
		days, readErr := counts.Read(t.Context(), adminstore.Daily, noon.Add(-24*time.Hour), noon)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if days.Total("views.people") != 5 || days.Total(statistics.MetricVisitors) != 2 {
			t.Fatalf("%s: days are %v", stage, days.Values)
		}
		shares := days.Breakdown(statistics.CountryPrefix)
		if len(shares) != 2 || shares[0].Name != "CZ" || shares[0].Value != 4 || shares[1].Name != "DE" {
			t.Fatalf("%s: countries are %v", stage, shares)
		}
	}
	stored := func(resolution adminstore.Resolution) int {
		t.Helper()
		rows, readErr := store.Counts(t.Context(), resolution, noon.Add(-48*time.Hour), noon.Add(48*time.Hour))
		if readErr != nil {
			t.Fatal(readErr)
		}
		return len(rows)
	}
	onDisk := func(stage string, hours, days int) {
		t.Helper()
		if gotHours, gotDays := stored(
			adminstore.Hourly,
		), stored(
			adminstore.Daily,
		); gotHours != hours ||
			gotDays != days {
			t.Fatalf("%s: the disk holds %d hours and %d days, want %d and %d", stage, gotHours, gotDays, hours, days)
		}
	}
	check("in memory")
	if err = counts.Flush(t.Context(), noon.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	onDisk("while the hour still runs", 0, 0)
	if err = counts.Flush(t.Context(), noon.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	onDisk("after the hours ended, the views alone", 2, 1)
	check("hours on disk")
	if err = counts.Flush(t.Context(), noon.Add(12*time.Hour)); err != nil {
		t.Fatal(err)
	}
	onDisk("after the day ended", 2, 4)
	check("all on disk")
	night := noon.Add(13 * time.Hour)
	counts.Add(statistics.CountryPrefix+"SK", 1, night)
	counts.Add("views.people", 1, night)
	if err = counts.Stop(t.Context(), night); err != nil {
		t.Fatal(err)
	}
	onDisk("a stopped server, whose running periods are no rows of a chart", 2, 4)
	carried, err := store.TakeCarried(t.Context())
	if err != nil || len(carried) != 2 {
		t.Fatalf("a stopped server set aside %v, %v", carried, err)
	}
	for _, count := range carried {
		if statistics.DailyOnly(count.Metric) && !count.Start.Equal(adminstore.Daily.Start(count.Start)) {
			t.Fatalf("a stopped server keeps the hour %s of %s", count.Start, count.Metric)
		}
	}
	if err = store.Carry(t.Context(), carried); err != nil {
		t.Fatal(err)
	}
	restarted := statistics.New(store)
	restarted.Enable(true)
	if err = restarted.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	days, err := restarted.Read(t.Context(), adminstore.Daily, noon.Add(-24*time.Hour), night)
	if err != nil || days.Total("views.people") != 6 || len(days.Breakdown(statistics.CountryPrefix)) != 3 {
		t.Fatalf("after a restart the days are %v, %v", days, err)
	}
	if err = statistics.New(store).Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = restarted.Flush(t.Context(), noon.Add(statistics.HourlyRetention+3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	onDisk("after the hourly retention, with the carried counts written once", 1, 6)
}
