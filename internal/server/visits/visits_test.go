package visits_test

import (
	"fmt"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/country"
	"github.com/misaon/nylzo/internal/server/visits"
)

const browser = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"

var start = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type recorded map[string]int64

func (r recorded) Add(metric string, value int64, _ time.Time) {
	r[metric] += value
}

type czech struct{}

func (czech) Lookup(netip.Addr) country.Code {
	return country.Code{'C', 'Z'}
}

func visit(signal, page, address, userAgent string, navigation bool) *visits.Visit {
	return &visits.Visit{
		Signal:     signal,
		Page:       page,
		Navigation: navigation,
		Address:    netip.MustParseAddr(address),
		UserAgent:  userAgent,
	}
}

func document(address string) *visits.Visit {
	return visit(api.VisitDocument, api.PageHome, address, browser, true)
}

func person(address string) *visits.Visit {
	return visit(api.VisitPerson, api.PageHome, address, browser, false)
}

func expect(t *testing.T, counts, want recorded) {
	t.Helper()
	for metric, value := range want {
		if counts[metric] != value {
			t.Errorf("%s is %d, want %d", metric, counts[metric], value)
		}
	}
	for metric, value := range counts {
		if _, wanted := want[metric]; !wanted && value != 0 {
			t.Errorf("%s is %d, want none", metric, value)
		}
	}
}

// A person is a browser whose page reported an interaction; a robot names itself or loads a page without
// navigating to it; a page that a browser loaded and nobody touched is neither.
func TestTrackerTellsPeopleRobotsAndUnconfirmedApart(t *testing.T) {
	counts := recorded{}
	tracker := visits.NewTracker(counts, czech{}, start)
	tracker.Record(document("203.0.113.1"), start)
	tracker.Record(person("203.0.113.1"), start.Add(time.Second))
	tracker.Record(person("203.0.113.1"), start.Add(time.Minute))
	tracker.Record(document("203.0.113.2"), start)
	googlebot := "Mozilla/5.0 (compatible; Googlebot/2.1)"
	tracker.Record(visit(api.VisitDocument, api.PageHome, "203.0.113.3", googlebot, true), start)
	tracker.Record(visit(api.VisitPerson, api.PageHome, "203.0.113.3", googlebot, false), start)
	tracker.Record(visit(api.VisitDocument, api.PageHome, "203.0.113.4", browser, false), start)
	tracker.Record(document("203.0.113.5"), start)
	tracker.Record(visit(api.VisitAutomation, api.PageHome, "203.0.113.5", browser, false), start)
	tracker.Record(visit(api.VisitPerson, "read", "203.0.113.6", browser, false), start)
	if live := tracker.Live(start.Add(2 * time.Minute)); live.Online != 1 {
		t.Errorf("%d people online, want 1", live.Online)
	}
	if live := tracker.Live(start.Add(7 * time.Minute)); live.Online != 0 {
		t.Errorf("%d people online six minutes after the last sign, want none", live.Online)
	}
	tracker.Settle(start.Add(9 * time.Minute))
	if counts[visits.MetricUnconfirmed] != 0 {
		t.Fatalf("a document counted as unconfirmed before its ten minutes: %v", counts)
	}
	tracker.Settle(start.Add(11 * time.Minute))
	expect(t, counts, recorded{
		visits.MetricPeople: 2, visits.MetricVisitors: 1, visits.CountryPrefix + "CZ": 1,
		visits.PagePrefix + api.PageHome: 2, visits.MetricUnconfirmed: 1, visits.MetricRobots: 3,
		visits.RobotPrefix + "search": 1, visits.RobotPrefix + "script": 1, visits.RobotPrefix + "headless": 1,
	})
}

// One view counts once whichever of its two signals comes first, on the page it belongs to, also when the
// person's sign comes after the document already counted as unconfirmed, and when the page loaded twice within
// seconds, as the home page does when it sends a reader on to their language.
func TestTrackerCountsAViewOnceInEitherOrder(t *testing.T) {
	counts := recorded{}
	tracker := visits.NewTracker(counts, czech{}, start)
	tracker.Record(person("203.0.113.1"), start)
	tracker.Record(document("203.0.113.1"), start.Add(2*time.Second))
	tracker.Record(document("203.0.113.2"), start)
	tracker.Record(visit(api.VisitPerson, api.PageTerms, "203.0.113.2", browser, false), start.Add(time.Second))
	tracker.Record(document("203.0.113.3"), start)
	tracker.Record(document("203.0.113.4"), start)
	tracker.Record(document("203.0.113.4"), start.Add(time.Second))
	tracker.Record(person("203.0.113.4"), start.Add(2*time.Second))
	tracker.Settle(start.Add(11 * time.Minute))
	if counts[visits.MetricUnconfirmed] != 2 {
		t.Fatalf(
			"unconfirmed is %d, want the home documents of the second and third visitor",
			counts[visits.MetricUnconfirmed],
		)
	}
	tracker.Record(person("203.0.113.3"), start.Add(12*time.Minute))
	expect(t, counts, recorded{
		visits.MetricPeople: 4, visits.MetricVisitors: 4, visits.CountryPrefix + "CZ": 4,
		visits.PagePrefix + api.PageHome: 3, visits.PagePrefix + api.PageTerms: 1, visits.MetricUnconfirmed: 1,
	})
}

// One visitor who reports more views in an hour than a person makes counts as a flood for the rest of that hour,
// and one address, or one /64 of IPv6, that shows more visitors in a day than a household has counts as a flood
// too.
func TestTrackerCountsFloodsAsRobots(t *testing.T) {
	counts := recorded{}
	tracker := visits.NewTracker(counts, czech{}, start)
	for range 61 {
		tracker.Record(person("203.0.113.1"), start)
	}
	tracker.Record(person("203.0.113.1"), start.Add(time.Hour))
	if counts[visits.MetricPeople] != 61 || counts[visits.RobotPrefix+"flood"] != 1 {
		t.Fatalf("one visitor: %v", counts)
	}
	for agent := range 70 {
		changing := fmt.Sprintf("%s Build/%d", browser, agent)
		tracker.Record(visit(api.VisitPerson, api.PageHome, "203.0.113.2", changing, false), start)
	}
	if counts[visits.MetricVisitors] != 1+64 || counts[visits.RobotPrefix+"flood"] != 1+6 {
		t.Fatalf("one address with many User-Agents: %v", counts)
	}
	for host := range 70 {
		tracker.Record(person(fmt.Sprintf("2001:db8:0:1::%x", host+1)), start)
	}
	if counts[visits.MetricVisitors] != 1+64+64 || counts[visits.RobotPrefix+"flood"] != 1+6+6 {
		t.Fatalf("one /64 with many addresses: %v", counts)
	}
}

// A visitor is forgotten when the UTC day ends, also when a signal comes before the minute's work: the same
// address and browser count as a new visitor, and a document that still waited counts as unconfirmed. A signal
// stamped before midnight that arrives after one stamped after it starts no day again. A server that stops counts
// the documents that wait, and one that forgets keeps no traffic of the last minutes either.
func TestTrackerForgetsVisitorsWhenTheDayEnds(t *testing.T) {
	counts := recorded{}
	evening := time.Date(2026, 11, 15, 23, 50, 0, 0, time.UTC)
	tracker := visits.NewTracker(counts, czech{}, evening)
	tracker.Record(person("2001:db8::1"), evening)
	tracker.Record(document("2001:db8::2"), evening.Add(5*time.Minute))
	morning := evening.Add(10*time.Minute + 30*time.Second)
	tracker.Record(person("2001:db8::1"), morning)
	if counts[visits.MetricVisitors] != 2 || counts[visits.MetricPeople] != 2 || counts[visits.MetricUnconfirmed] != 1 {
		t.Fatalf("counted %v", counts)
	}
	tracker.Record(person("2001:db8::1"), evening.Add(9*time.Minute))
	if counts[visits.MetricVisitors] != 2 {
		t.Fatalf("a late signal of the day before started the day again: %v", counts)
	}
	tracker.Record(document("2001:db8:0:2::1"), morning)
	tracker.Stop(morning)
	if counts[visits.MetricUnconfirmed] != 2 {
		t.Fatalf("a stopping tracker left its waiting document uncounted: %v", counts)
	}
	tracker.Forget()
	live := tracker.Live(morning)
	if live.Online != 0 || slices.Max(live.People) != 0 {
		t.Fatalf("after the tracker forgot it still shows %d online and the views %v", live.Online, live.People)
	}
}

func TestRobotFamily(t *testing.T) {
	cases := map[string]visits.Family{
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":   visits.FamilySearch,
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2)": visits.FamilyAI,
		"Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)":                 visits.FamilyPreview,
		"Mozilla/5.0 (X11; Linux x86_64) HeadlessChrome/140.0.0.0 Safari/537.36":     visits.FamilyHeadless,
		"curl/8.17.0": visits.FamilyScript,
		"":            visits.FamilyScript,
		"Mozilla/5.0 (compatible; SomethingNewBot/1.0)": visits.FamilyOther,
	}
	for userAgent, want := range cases {
		if family, robot := visits.RobotFamily(userAgent); !robot || family != want {
			t.Errorf("%q is %q, %v; want %q", userAgent, family, robot, want)
		}
	}
	for _, userAgent := range []string{
		browser,
		"Mozilla/5.0 (Linux; Android 14; CUBOT KingKong 9) AppleWebKit/537.36 Chrome/140.0.0.0 Mobile Safari/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 [FBAN/FBIOS;FB_IAB/FB4A]",
		"Mozilla/5.0 (Linux; Android 15) AppleWebKit/537.36 Chrome/142.0.0.0 Mobile Safari/537.36 Viber/22.0",
	} {
		if family, robot := visits.RobotFamily(userAgent); robot {
			t.Errorf("%q counts as the robot %q", userAgent, family)
		}
	}
}
