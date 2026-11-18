package ratelimit_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/ratelimit"
)

// The address of a refused client stays for an hour after its last refusal and goes when the day's tables go; every
// change moves the version, which the admin page holds instead of fetching the list again.
func TestRefusalsForgetAClientAnHourAfterItsLastRefusal(t *testing.T) {
	refusals := ratelimit.NewRefusals()
	start := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	refusals.Note(netip.MustParseAddr("203.0.113.7"), "create", start)
	refusals.Note(netip.MustParseAddr("203.0.113.7"), "requests", start.Add(30*time.Minute))
	refusals.Note(netip.MustParseAddr("2001:db8:1:2:3:4:5:6"), "read", start)
	refusals.Note(netip.MustParseAddr("2001:db8:1:2::9"), "read", start.Add(time.Minute))
	listed := refusals.List()
	if len(listed) != 2 || listed[0].Client.String() != "203.0.113.7/32" || listed[0].Count != 2 ||
		listed[1].Client.String() != "2001:db8:1:2::/64" || listed[1].Reasons["read"] != 2 {
		t.Fatalf("listed %v", listed)
	}
	noted := refusals.Version()
	refusals.Purge(start.Add(61 * time.Minute))
	if listed = refusals.List(); len(listed) != 1 || listed[0].Client.Addr().Is6() || refusals.Version() == noted {
		t.Fatalf("after an hour: %v", listed)
	}
	purged := refusals.Version()
	refusals.Purge(start.Add(61 * time.Minute))
	if refusals.Version() != purged {
		t.Fatal("a purge that dropped nothing moved the version")
	}
	refusals.Reset()
	if listed = refusals.List(); len(listed) != 0 || refusals.Version() == purged {
		t.Fatalf("after the reset: %v", listed)
	}
}
