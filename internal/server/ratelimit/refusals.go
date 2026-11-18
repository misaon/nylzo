package ratelimit

import (
	"cmp"
	"maps"
	"net/netip"
	"slices"
	"sync"
	"time"
)

const (
	// refusalMemory is how long after its last refusal a client stays on the list.
	refusalMemory = time.Hour
	maxRefused    = 256
)

// Refused is a client that a limit turned away: its /32 or /64, in the clear, so the operator can act on it.
type Refused struct {
	Client  netip.Prefix
	Reasons map[string]int64
	First   time.Time
	Last    time.Time
	Count   int64
}

// Refusals lists the clients the limits refused within the last hour. It lives in memory only and is emptied when
// a UTC day begins, like every other table that knows an address (spec §5.3).
type Refusals struct {
	mutex   sync.Mutex
	clients map[netip.Prefix]*Refused
	version int64
}

func NewRefusals() *Refusals {
	return &Refusals{clients: make(map[netip.Prefix]*Refused)}
}

func (r *Refusals) Note(address netip.Addr, reason string, now time.Time) {
	address = address.Unmap()
	bits := int(sourcePrefixIPv6)
	if address.Is4() {
		bits = int(sourcePrefixIPv4)
	}
	client, err := address.Prefix(bits)
	if err != nil {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	entry, ok := r.clients[client]
	if !ok {
		if len(r.clients) >= maxRefused {
			r.dropOldest()
		}
		entry = &Refused{Client: client, Reasons: make(map[string]int64), First: now}
		r.clients[client] = entry
	}
	entry.Reasons[reason]++
	entry.Count++
	entry.Last = now
	r.version++
}

// Version changes with every change of the list, so a reader that holds the list of a version need not fetch it again.
func (r *Refusals) Version() int64 {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.version
}

// List copies the entries, the latest refusal first.
func (r *Refusals) List() []Refused {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	listed := make([]Refused, 0, len(r.clients))
	for _, entry := range r.clients {
		copied := *entry
		copied.Reasons = maps.Clone(entry.Reasons)
		listed = append(listed, copied)
	}
	slices.SortFunc(listed, func(a, b Refused) int {
		return cmp.Or(b.Last.Compare(a.Last), a.Client.Addr().Compare(b.Client.Addr()))
	})
	return listed
}

func (r *Refusals) Purge(now time.Time) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	held := len(r.clients)
	r.clients = Rebuilt(r.clients, func(entry *Refused) bool { return now.Sub(entry.Last) < refusalMemory })
	if len(r.clients) != held {
		r.version++
	}
}

func (r *Refusals) Reset() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.clients = make(map[netip.Prefix]*Refused)
	r.version++
}

func (r *Refusals) dropOldest() {
	var oldest *Refused
	for _, entry := range r.clients {
		if oldest == nil || entry.Last.Before(oldest.Last) {
			oldest = entry
		}
	}
	if oldest != nil {
		delete(r.clients, oldest.Client)
	}
}
