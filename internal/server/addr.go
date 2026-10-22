package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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
	start, end   int64
	count, bytes int64
}

type addrCounters struct {
	mu     sync.Mutex
	max    int
	tables map[string]map[bucket]addrCount
}

func newAddrCounters() *addrCounters {
	return &addrCounters{max: maxCounterBuckets, tables: map[string]map[bucket]addrCount{}}
}

func (c *addrCounters) hit(hits []addrHit, now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	at := now.UnixNano()
	for _, h := range hits {
		table := c.tables[h.kind]
		if table == nil {
			table = map[bucket]addrCount{}
			c.tables[h.kind] = table
		}
		start := h.window.start.UnixNano()
		e, ok := table[h.bucket]
		if !ok || e.start != start {
			if !ok && len(table) >= c.max {
				c.evict(table, at)
			}
			e = addrCount{start: start, end: h.window.end.UnixNano()}
		}
		e.count++
		e.bytes += h.bytes
		table[h.bucket] = e
		if (h.limit > 0 && e.count > h.limit) || (h.bytesLimit > 0 && e.bytes > h.bytesLimit) {
			return max(h.window.end.Sub(now), time.Second)
		}
	}
	return 0
}

func (c *addrCounters) evict(table map[bucket]addrCount, at int64) {
	for k, e := range table {
		if at >= e.end {
			delete(table, k)
		}
	}
	for k := range table {
		if len(table) < c.max-c.max/evictDivisor {
			return
		}
		delete(table, k)
	}
}
