// Package statistics counts what the admin area charts. A count waits in memory until its period has ended, so the
// charts' rows never tell when within an hour, or for a daily metric within a day, something happened; nothing
// here knows an address or a message.
package statistics

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/misaon/nylzo/internal/server/adminstore"
)

const (
	// HourlyRetention keeps the hours a seven-day chart draws and the seven days it compares them with.
	HourlyRetention = 15 * 24 * time.Hour
	// DailyRetention keeps the days a twelve-month chart draws, and the month before them.
	DailyRetention = 400 * 24 * time.Hour
	// waitingLimit is how long after its period a count that could not be written waits for a later flush.
	waitingLimit = 48 * time.Hour
)

const (
	// MetricVisitors counts the distinct people of a UTC day.
	MetricVisitors = "visitors"
	// CountryPrefix starts the count of the day's visitors from a country.
	CountryPrefix = "country."

	MetricCreated = "messages.created"
	// MetricOpened counts the claims a reader acknowledged.
	MetricOpened = "messages.opened"
	// MetricEnded counts the burns and cancels.
	MetricEnded = "messages.ended"
	// MetricExpired counts the blobs the sweep deleted.
	MetricExpired = "messages.expired"
	// MetricRefusals counts the requests a limit refused.
	MetricRefusals = "refusals"
)

// DailyOnly tells the metrics that are kept by the day only: the hour of a first visit beside the hour of a created
// message would tell the country of a sender on a quiet instance.
func DailyOnly(metric string) bool {
	return metric == MetricVisitors || strings.HasPrefix(metric, CountryPrefix)
}

type period struct {
	start  int64
	metric string
}

// end is the end of the hour of the count, or of its UTC day for a metric that is kept by the day only.
func (p period) end() time.Time {
	resolution := adminstore.Hourly
	if DailyOnly(p.metric) {
		resolution = adminstore.Daily
	}
	return resolution.Start(time.Unix(p.start, 0)).Add(time.Duration(resolution) * time.Second)
}

// Counts holds the counts of the periods that still run. Without a store it keeps nothing.
type Counts struct {
	store   *adminstore.Store
	enabled atomic.Bool
	// flushing keeps a read from falling between the moment a flush takes the waiting counts and its commit.
	flushing sync.RWMutex
	mutex    sync.Mutex
	waiting  map[period]int64
}

func New(store *adminstore.Store) *Counts {
	return &Counts{store: store, waiting: make(map[period]int64)}
}

// Enable switches counting on or off; a server whose admin area nobody set up counts nothing.
func (c *Counts) Enable(enabled bool) {
	c.enabled.Store(enabled && c.store != nil)
}

func (c *Counts) Enabled() bool {
	return c.enabled.Load()
}

func (c *Counts) Add(metric string, value int64, now time.Time) {
	if !c.Enabled() || value == 0 {
		return
	}
	// A metric that is kept by the day only never has an hour, in memory or where a stop sets it aside.
	resolution := adminstore.Hourly
	if DailyOnly(metric) {
		resolution = adminstore.Daily
	}
	key := period{start: resolution.Start(now).Unix(), metric: metric}
	c.mutex.Lock()
	c.waiting[key] += value
	c.mutex.Unlock()
}

// Flush writes the counts whose period has ended and drops the periods past their retention. A count that could
// not be written waits for the next flush, for two days after its period at most.
func (c *Counts) Flush(ctx context.Context, now time.Time) error {
	if c.store == nil {
		return nil
	}
	c.flushing.Lock()
	defer c.flushing.Unlock()
	due := c.take(func(key period) bool { return !key.end().After(now) })
	err := c.store.AddCounts(ctx, countsOf(due))
	if err != nil && !errors.Is(err, adminstore.ErrJournalNotErased) {
		c.putBack(due, now.Add(-waitingLimit))
		return err
	}
	return errors.Join(err, c.store.ForgetCounts(ctx, now.Add(-HourlyRetention), now.Add(-DailyRetention)))
}

// Stop writes what Flush would and sets the counts of the periods that still run aside for the next start, so a
// restart loses none of them and the rows of the charts still hold whole periods only.
func (c *Counts) Stop(ctx context.Context, now time.Time) error {
	if c.store == nil {
		return nil
	}
	err := c.Flush(ctx, now)
	c.flushing.Lock()
	defer c.flushing.Unlock()
	running := c.take(func(period) bool { return true })
	carryErr := c.store.Carry(ctx, countsOf(running))
	if carryErr != nil && !errors.Is(carryErr, adminstore.ErrJournalNotErased) {
		c.putBack(running, now.Add(-waitingLimit))
	}
	return errors.Join(err, carryErr)
}

// Restore takes back the counts the last Stop set aside, however long the server was stopped.
func (c *Counts) Restore(ctx context.Context) error {
	if c.store == nil {
		return nil
	}
	carried, err := c.store.TakeCarried(ctx)
	restored := make(map[period]int64, len(carried))
	for _, count := range carried {
		restored[period{start: count.Start.Unix(), metric: count.Metric}] += count.Value
	}
	c.putBack(restored, time.Time{})
	return err
}

func countsOf(taken map[period]int64) []adminstore.Count {
	counts := make([]adminstore.Count, 0, len(taken))
	for key, value := range taken {
		counts = append(counts, adminstore.Count{
			Start:     time.Unix(key.start, 0),
			Metric:    key.metric,
			Value:     value,
			DailyOnly: DailyOnly(key.metric),
		})
	}
	return counts
}

// Series holds, for each metric, one value per period from From on.
type Series struct {
	Resolution adminstore.Resolution
	From       time.Time
	Periods    int
	Values     map[string][]int64
}

// Total sums a metric over the series.
func (s *Series) Total(metric string) int64 {
	var total int64
	for _, value := range s.Values[metric] {
		total += value
	}
	return total
}

type Share struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

// Breakdown lists the metrics that start with prefix, without it, with their totals, largest first.
func (s *Series) Breakdown(prefix string) []Share {
	var shares []Share
	for metric := range s.Values {
		name, ok := strings.CutPrefix(metric, prefix)
		if total := s.Total(metric); ok && name != "" && total > 0 {
			shares = append(shares, Share{Name: name, Value: total})
		}
	}
	slices.SortFunc(shares, func(a, b Share) int {
		return cmp.Or(cmp.Compare(b.Value, a.Value), strings.Compare(a.Name, b.Name))
	})
	return shares
}

// Read gives the periods of a resolution that start in [from, until), with the counts that still wait. The hours
// hold no daily-only metric.
func (c *Counts) Read(
	ctx context.Context,
	resolution adminstore.Resolution,
	from, until time.Time,
) (*Series, error) {
	from = resolution.Start(from)
	periods := int((until.Unix() - from.Unix() + int64(resolution) - 1) / int64(resolution))
	series := &Series{Resolution: resolution, From: from, Periods: periods, Values: make(map[string][]int64)}
	if c.store == nil {
		return series, nil
	}
	c.flushing.RLock()
	defer c.flushing.RUnlock()
	stored, err := c.store.Counts(ctx, resolution, from, until)
	if err != nil {
		return nil, err
	}
	for _, count := range stored {
		series.add(count.Start, count.Metric, count.Value)
	}
	c.mutex.Lock()
	waiting := maps.Clone(c.waiting)
	c.mutex.Unlock()
	for key, value := range waiting {
		if resolution == adminstore.Daily || !DailyOnly(key.metric) {
			series.add(resolution.Start(time.Unix(key.start, 0)), key.metric, value)
		}
	}
	return series, nil
}

func (c *Counts) take(chosen func(key period) bool) map[period]int64 {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	taken := make(map[period]int64)
	for key, value := range c.waiting {
		if chosen(key) {
			taken[key] = value
			delete(c.waiting, key)
		}
	}
	return taken
}

// putBack makes the counts wait again, apart from those whose period ended before the limit.
func (c *Counts) putBack(counts map[period]int64, limit time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for key, value := range counts {
		if key.end().After(limit) {
			c.waiting[key] += value
		}
	}
}

func (s *Series) add(start time.Time, metric string, value int64) {
	index := int((start.Unix() - s.From.Unix()) / int64(s.Resolution))
	if index < 0 || index >= s.Periods {
		return
	}
	values, ok := s.Values[metric]
	if !ok {
		values = make([]int64, s.Periods)
		s.Values[metric] = values
	}
	values[index] += value
}
