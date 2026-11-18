package adminapi

import (
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/statistics"
)

// A trend compares the running span with as much of the span before as has passed of it, so a quarter of the running
// hour is set against a quarter of the hour it matches, not the whole hour.
func TestATrendComparesTheElapsedPartOfThePreviousSpan(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 18, 12, 15, 0, 0, time.UTC)
	hours := &statistics.Series{
		Resolution: adminstore.Hourly,
		From:       now.Add(-time.Hour).Truncate(time.Hour),
		Periods:    2,
		Values:     map[string][]int64{statistics.MetricCreated: {6, 8}},
	}
	days := &statistics.Series{
		Resolution: adminstore.Daily,
		From:       adminstore.Daily.Start(now),
		Periods:    1,
		Values:     map[string][]int64{statistics.MetricVisitors: {86400}},
	}
	previous := totals(elapsedPart(spanCounts{periods: hours, days: days}, now))
	if got := previous[statistics.MetricCreated]; got != 6+2 {
		t.Fatalf("the span before counts %d, want the full first hour and a quarter of the second", got)
	}
	if got := previous[statistics.MetricVisitors]; got != 12*3600+15*60 {
		t.Fatalf("the day-only metric counts %d, want the part of the day that has passed", got)
	}
}
