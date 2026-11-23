package visits

import (
	"net/netip"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/country"
)

const fullTableBrowser = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"

type tally map[string]int64

func (t tally) Add(metric string, value int64, _ time.Time) {
	t[metric] += value
}

type unknownCountries struct{}

func (unknownCountries) Lookup(netip.Addr) country.Code {
	return country.Code{}
}

func browserSignal(signal string) *Visit {
	return &Visit{
		Signal:     signal,
		Page:       api.PageHome,
		Navigation: signal == api.VisitDocument,
		Address:    netip.MustParseAddr("203.0.113.9"),
		UserAgent:  fullTableBrowser,
	}
}

func TestAFullPairingTableCountsADocumentAsUnconfirmedUntilAPersonShowsUp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	counts := tally{}
	tracker := NewTracker(counts, unknownCountries{}, now)
	for i := range maxPairings {
		tracker.pairings[view{who: visitor(i), page: "filler"}] = &pairing{since: now}
	}
	tracker.Record(browserSignal(api.VisitDocument), now)
	if counts[MetricUnconfirmed] != 1 {
		t.Fatalf("a document without room in the table: %v", counts)
	}
	tracker.Record(browserSignal(api.VisitPerson), now)
	tracker.Record(browserSignal(api.VisitPerson), now)
	if counts[MetricUnconfirmed] != 0 || counts[MetricPeople] != 2 || tracker.unpaired != 0 {
		t.Fatalf("people after a document without room: %v, %d unpaired", counts, tracker.unpaired)
	}
}

func TestAFullVisitorTableStillCountsTheViewOfANewSource(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	counts := tally{}
	tracker := NewTracker(counts, unknownCountries{}, now)
	for i := range maxVisitors {
		tracker.people[visitor(i)] = &person{}
	}
	tracker.Record(browserSignal(api.VisitPerson), now)
	if counts[MetricPeople] != 1 || counts[MetricVisitors] != 0 || len(tracker.online) != 0 {
		t.Fatalf("a view without room for its visitor: %v, %d online", counts, len(tracker.online))
	}
}

func TestASignalOfAnUnknownKindCountsNothing(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	counts := tally{}
	NewTracker(counts, unknownCountries{}, now).Record(browserSignal("unknown"), now)
	if len(counts) != 0 {
		t.Fatalf("an unknown signal counted %v", counts)
	}
}
