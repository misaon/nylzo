package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"math"
	"net/netip"
	"sync"
	"time"
)

const (
	bucketLen         = 3
	maxCounterBuckets = 1 << 16
)

type bucket [bucketLen]byte

type addrKey [32]byte

func newAddrKey() *addrKey {
	var k addrKey
	rand.Read(k[:])
	return &k
}

func (k *addrKey) bucket(addr netip.Addr, coarse bool) bucket {
	addr = addr.Unmap()
	family, plen := byte(6), 64
	if coarse {
		plen = 48
	}
	if addr.Is4() {
		family, plen = 4, 32
		if coarse {
			plen = 24
		}
	}
	prefix, _ := addr.Prefix(plen)
	m := hmac.New(sha256.New, k[:])
	m.Write([]byte{family, byte(plen)})
	m.Write(prefix.Addr().AsSlice())
	return bucket(m.Sum(nil)[:bucketLen])
}

type addrHit struct {
	kind       string
	bucket     bucket
	window     window
	bytes      int64
	limit      int64
	bytesLimit int64
}

type addrCount struct {
	start uint32
	count uint32
	bytes int64
}

type addrTable struct {
	length  int64
	entries map[bucket]addrCount
}

type addrCounters struct {
	mu     sync.Mutex
	max    int
	tables map[string]*addrTable
}

func newAddrCounters() *addrCounters {
	return &addrCounters{max: maxCounterBuckets, tables: map[string]*addrTable{}}
}

func (c *addrCounters) hit(hits []addrHit, now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, h := range hits {
		table := c.tables[h.kind]
		if table == nil {
			table = &addrTable{
				length:  int64(h.window.end.Sub(h.window.start) / time.Second),
				entries: map[bucket]addrCount{},
			}
			c.tables[h.kind] = table
		}
		start := uint32(h.window.start.Unix())
		e, ok := table.entries[h.bucket]
		if !ok || e.start != start {
			if !ok && len(table.entries) >= c.max {
				at := now.Unix()
				evictTo(table.entries, c.max, func(e addrCount) bool { return at >= int64(e.start)+table.length })
			}
			e = addrCount{start: start}
		}
		e.count = min(e.count, math.MaxUint32-1) + 1
		e.bytes += h.bytes
		table.entries[h.bucket] = e
		if (h.limit > 0 && int64(e.count) > h.limit) || (h.bytesLimit > 0 && e.bytes > h.bytesLimit) {
			return max(h.window.end.Sub(now), time.Second)
		}
	}
	return 0
}
