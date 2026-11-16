// Package visits tells people from robots among the visits of the public pages and counts both, without keeping
// an address: a visitor is a keyed hash that lives in memory until the UTC day ends.
package visits

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/country"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/statistics"
)

const (
	MetricPeople      = "views.people"
	MetricRobots      = "views.robots"
	MetricUnconfirmed = "views.unconfirmed"
	MetricVisitors    = statistics.MetricVisitors
	PagePrefix        = "page."
	CountryPrefix     = statistics.CountryPrefix
	RobotPrefix       = "robot."
	unknownCountry    = "unknown"
)

const (
	// confirmWithin is how long a page waits for a sign of a person before it counts as unconfirmed, and how long
	// a sign of a person waits for the report of its page.
	confirmWithin = 10 * time.Minute
	// sameViewWithin is how soon a page loaded again is still the same view: the home page loads twice when it
	// sends a reader on to the language they chose before.
	sameViewWithin = 10 * time.Second
	// onlineFor is how long after the last sign a person counts as online.
	onlineFor = 5 * time.Minute
	// liveMinutes is the length of the per-minute history.
	liveMinutes = 30
	// viewsPerHour is where one visitor stops counting as a person and starts counting as a flood.
	viewsPerHour = 60
	// visitorsPerSource is how many visitors one address, or one /64 of IPv6, may be in a day; more are one client
	// that changes its User-Agent or its address to look like many.
	visitorsPerSource = 64
	sourceBits        = 64
	maxVisitors       = 200_000
	maxPairings       = 50_000
	sourceLabel       = "source"
)

type Recorder interface {
	Add(metric string, value int64, now time.Time)
}

type Countries interface {
	Lookup(address netip.Addr) country.Code
}

// Visit is one signal about a page: whoever served the page reports the document, the page reports a person.
type Visit struct {
	Signal     string
	Page       string
	Navigation bool
	Address    netip.Addr
	UserAgent  string
}

type visitor uint64

type person struct {
	hour  int64
	views int
}

type view struct {
	who  visitor
	page string
}

// A pairing matches the report of a page with the sign of a person on it, whichever comes first.
type pairing struct {
	since time.Time
	// latest is when the last document of the view was reported.
	latest time.Time
	// documents wait for a sign of a person.
	documents int64
	// people gave a sign before the report of their page came.
	people int64
	// unconfirmed documents of today, which a late sign of a person still turns into a person's view.
	unconfirmed int64
}

type minute struct {
	start  int64
	people int
	robots int
}

type Tracker struct {
	recorder  Recorder
	countries Countries
	mutex     sync.Mutex
	key       [sha256.Size]byte
	day       time.Time
	people    map[visitor]*person
	sources   map[visitor]int
	online    map[visitor]time.Time
	pairings  map[view]*pairing
	// unpaired counts the documents of today that the full table of pairings had no room for and that counted as
	// unconfirmed at once; a sign of a person that finds no pairing takes one of them back.
	unpaired int64
	minutes  [liveMinutes]minute
}

func NewTracker(recorder Recorder, countries Countries, now time.Time) *Tracker {
	tracker := &Tracker{recorder: recorder, countries: countries, day: ratelimit.Day(now).Start}
	tracker.startOver()
	return tracker
}

// Record counts one signal. A robot that names itself counts at once; a document a browser asked for waits for a
// sign of a person, and counts as unconfirmed when none comes.
func (t *Tracker) Record(visit *Visit, now time.Time) {
	if !slices.Contains(api.VisitPages, visit.Page) {
		return
	}
	family, robot := RobotFamily(visit.UserAgent)
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.startDay(now)
	who := t.hash(visit.Address.Unmap().AsSlice(), visit.UserAgent)
	seen := view{who: who, page: visit.Page}
	switch {
	case visit.Signal == api.VisitDocument && robot:
		t.countRobot(family, now)
	case visit.Signal == api.VisitDocument && !visit.Navigation:
		t.countRobot(FamilyScript, now)
	case visit.Signal == api.VisitDocument:
		t.document(seen, now)
	case robot:
	case visit.Signal == api.VisitAutomation:
		t.confirm(seen, now)
		t.countRobot(FamilyHeadless, now)
	case visit.Signal == api.VisitPerson:
		t.confirm(seen, now)
		t.countPerson(who, visit, now)
	default:
	}
}

// Settle counts the documents that waited too long as unconfirmed, forgets who was online five minutes ago and,
// when a UTC day begins, every visitor. It replaces the tables, so the memory of the dropped entries can be erased.
// While the pairings are more than half full, the ones that only remember an unconfirmed document make room, so
// a flood of documents fills the table for minutes and not for the rest of the day.
func (t *Tracker) Settle(now time.Time) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.startDay(now)
	t.online = ratelimit.Rebuilt(t.online, func(lastSeen time.Time) bool { return now.Sub(lastSeen) < onlineFor })
	crowded := len(t.pairings) > maxPairings/2
	t.pairings = ratelimit.Rebuilt(t.pairings, func(pair *pairing) bool {
		if now.Sub(pair.since) >= confirmWithin {
			t.recorder.Add(MetricUnconfirmed, pair.documents, now)
			pair.unconfirmed += pair.documents
			pair.documents, pair.people = 0, 0
		}
		return pair.documents > 0 || pair.people > 0 || (pair.unconfirmed > 0 && !crowded)
	})
}

// Forget drops every visitor and the traffic of the last minutes and draws a new key, so a server that stops
// counting keeps nothing about its visitors.
func (t *Tracker) Forget() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.startOver()
	t.minutes = [liveMinutes]minute{}
}

// Stop counts the documents that still wait for a sign of a person as unconfirmed, because a server that stops
// cannot wait for one.
func (t *Tracker) Stop(now time.Time) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for _, pair := range t.pairings {
		t.recorder.Add(MetricUnconfirmed, pair.documents, now)
		pair.documents = 0
	}
}

// Live is the traffic of the last minutes, oldest first.
type Live struct {
	Online int   `json:"online"`
	People []int `json:"people"`
	Robots []int `json:"robots"`
}

func (t *Tracker) Live(now time.Time) Live {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	live := Live{People: make([]int, liveMinutes), Robots: make([]int, liveMinutes)}
	for _, lastSeen := range t.online {
		if now.Sub(lastSeen) < onlineFor {
			live.Online++
		}
	}
	current := now.Unix() / int64(time.Minute/time.Second)
	for index := range liveMinutes {
		start := current - int64(liveMinutes-1-index)
		if slot := t.minutes[start%liveMinutes]; slot.start == start {
			live.People[index], live.Robots[index] = slot.people, slot.robots
		}
	}
	return live
}

// startDay ends the day before when a UTC day has begun: the documents that still wait count as unconfirmed in
// its last hour, and no visitor of it can be recognised again.
func (t *Tracker) startDay(now time.Time) {
	today := ratelimit.Day(now).Start
	if !today.After(t.day) {
		return
	}
	for _, pair := range t.pairings {
		t.recorder.Add(MetricUnconfirmed, pair.documents, today.Add(-time.Second))
	}
	t.day = today
	t.startOver()
}

func (t *Tracker) startOver() {
	rand.Read(t.key[:])
	t.people = make(map[visitor]*person)
	t.sources = make(map[visitor]int)
	t.online = make(map[visitor]time.Time)
	t.pairings = make(map[view]*pairing)
	t.unpaired = 0
}

// sourceOf names what the rate limits treat as one client: an IPv4 address or the /64 of an IPv6 address.
func (t *Tracker) sourceOf(address netip.Addr) visitor {
	address = address.Unmap()
	if prefix, err := address.Prefix(sourceBits); err == nil && address.Is6() {
		address = prefix.Addr()
	}
	return t.hash(address.AsSlice(), sourceLabel)
}

func (t *Tracker) hash(address []byte, userAgent string) visitor {
	mac := hmac.New(sha256.New, t.key[:])
	mac.Write(address)
	mac.Write([]byte{0})
	mac.Write([]byte(userAgent))
	return visitor(binary.BigEndian.Uint64(mac.Sum(nil)))
}

func (t *Tracker) pairingOf(seen view, now time.Time) *pairing {
	pair, ok := t.pairings[seen]
	if !ok && len(t.pairings) < maxPairings {
		pair = &pairing{since: now}
		t.pairings[seen] = pair
	}
	return pair
}

func (t *Tracker) document(seen view, now time.Time) {
	pair := t.pairingOf(seen, now)
	if pair != nil {
		repeated := now.Sub(pair.latest) < sameViewWithin
		pair.latest = now
		if repeated {
			return
		}
	}
	switch {
	case pair == nil:
		t.unpaired++
		t.recorder.Add(MetricUnconfirmed, 1, now)
	case pair.people > 0:
		pair.people--
	default:
		if pair.documents == 0 {
			pair.since = now
		}
		pair.documents++
	}
}

// confirm takes the document the sign belongs to out of the wait, or out of the unconfirmed views when it had
// waited too long, or remembers the sign for a report that is still on its way.
func (t *Tracker) confirm(seen view, now time.Time) {
	pair := t.pairingOf(seen, now)
	switch {
	case pair == nil && t.unpaired > 0:
		t.unpaired--
		t.recorder.Add(MetricUnconfirmed, -1, now)
	case pair == nil:
	case pair.documents > 0:
		pair.documents--
	case pair.unconfirmed > 0:
		pair.unconfirmed--
		t.recorder.Add(MetricUnconfirmed, -1, now)
	default:
		if pair.people == 0 {
			pair.since = now
		}
		pair.people++
	}
}

func (t *Tracker) countRobot(family Family, now time.Time) {
	t.recorder.Add(MetricRobots, 1, now)
	t.recorder.Add(RobotPrefix+string(family), 1, now)
	t.minuteOf(now).robots++
}

// countPerson counts a view. A visitor the full table has no room for still counts as a view, only not as a new
// visitor and not as online, so filling the table hides nobody; a source the table already knows gets no further
// visitor then, so the client that filled it gains nothing.
func (t *Tracker) countPerson(who visitor, visit *Visit, now time.Time) {
	entry, seen := t.people[who]
	if !seen {
		source := t.sourceOf(visit.Address)
		full := len(t.people) >= maxVisitors
		if t.sources[source] >= visitorsPerSource || (full && t.sources[source] > 0) {
			t.countRobot(FamilyFlood, now)
			return
		}
		if !full {
			t.sources[source]++
			entry = &person{}
			t.people[who] = entry
		}
	}
	if entry != nil && !t.withinHourlyViews(entry, now) {
		t.countRobot(FamilyFlood, now)
		return
	}
	t.recorder.Add(MetricPeople, 1, now)
	t.recorder.Add(PagePrefix+visit.Page, 1, now)
	t.minuteOf(now).people++
	if entry == nil {
		return
	}
	t.online[who] = now
	if seen {
		return
	}
	t.recorder.Add(MetricVisitors, 1, now)
	name := unknownCountry
	if code := t.countries.Lookup(visit.Address); code.Known() {
		name = code.String()
	}
	t.recorder.Add(CountryPrefix+name, 1, now)
}

func (*Tracker) withinHourlyViews(entry *person, now time.Time) bool {
	if hour := now.Unix() / int64(time.Hour/time.Second); hour != entry.hour {
		entry.hour, entry.views = hour, 0
	}
	entry.views++
	return entry.views <= viewsPerHour
}

func (t *Tracker) minuteOf(now time.Time) *minute {
	start := now.Unix() / int64(time.Minute/time.Second)
	slot := &t.minutes[start%liveMinutes]
	if slot.start != start {
		*slot = minute{start: start}
	}
	return slot
}
