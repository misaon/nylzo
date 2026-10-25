package ratelimit

import (
	"math"
	"sync"
	"time"
)

type Window struct {
	Start time.Time
	End   time.Time
}

func (w Window) PurgeAt() time.Time {
	return w.End.Add(w.End.Sub(w.Start))
}

func Hour(now time.Time) Window {
	start := now.UTC().Truncate(time.Hour)
	return Window{Start: start, End: start.Add(time.Hour)}
}

func Day(now time.Time) Window {
	utc := now.UTC()
	start := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	return Window{Start: start, End: start.Add(24 * time.Hour)}
}

type Hit struct {
	Kind       string
	Bucket     Bucket
	Window     Window
	Bytes      int64
	Limit      int64
	BytesLimit int64
}

type Counters struct {
	mutex      sync.Mutex
	maxEntries int
	tables     map[string]*counterTable
}

type counterTable struct {
	windowSeconds int64
	entries       map[Bucket]counter
}

type counter struct {
	start int64
	count uint32
	bytes int64
}

func NewCounters() *Counters {
	return &Counters{maxEntries: maxTableEntries, tables: map[string]*counterTable{}}
}

// Count stops at the first hit over its limit, so a refused request spends no later budget.
func (c *Counters) Count(hits []Hit, now time.Time) time.Duration {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for i := range hits {
		hit := &hits[i]
		entry := c.add(hit, now)
		overCount := hit.Limit > 0 && int64(entry.count) > hit.Limit
		overBytes := hit.BytesLimit > 0 && entry.bytes > hit.BytesLimit
		if overCount || overBytes {
			return max(hit.Window.End.Sub(now), time.Second)
		}
	}
	return 0
}

func (c *Counters) add(hit *Hit, now time.Time) counter {
	table := c.table(hit)
	start := hit.Window.Start.Unix()
	entry, ok := table.entries[hit.Bucket]
	if !ok || entry.start != start {
		if !ok && len(table.entries) >= c.maxEntries {
			nowSeconds := now.Unix()
			Trim(
				table.entries,
				c.maxEntries,
				func(old counter) bool { return nowSeconds >= old.start+table.windowSeconds },
			)
		}
		entry = counter{start: start}
	}
	entry.count = min(entry.count, math.MaxUint32-1) + 1
	entry.bytes += hit.Bytes
	table.entries[hit.Bucket] = entry
	return entry
}

func (c *Counters) table(hit *Hit) *counterTable {
	table, ok := c.tables[hit.Kind]
	if !ok {
		table = &counterTable{
			windowSeconds: int64(hit.Window.End.Sub(hit.Window.Start) / time.Second),
			entries:       map[Bucket]counter{},
		}
		c.tables[hit.Kind] = table
	}
	return table
}
