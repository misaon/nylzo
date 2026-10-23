package server

import "sync"

const (
	maxStreamsPerSource = 2
	maxStreamsPerRange  = 8
)

type streamSlots struct {
	mu     sync.Mutex
	total  int
	max    int
	source map[bucket]int
	ranges map[bucket]int
}

func newStreamSlots(maxStreams int) *streamSlots {
	return &streamSlots{max: maxStreams, source: map[bucket]int{}, ranges: map[bucket]int{}}
}

func (s *streamSlots) take(source, ranges bucket) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total >= s.max || s.source[source] >= maxStreamsPerSource || s.ranges[ranges] >= maxStreamsPerRange {
		return false
	}
	s.total++
	s.source[source]++
	s.ranges[ranges]++
	return true
}

func (s *streamSlots) release(source, ranges bucket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total--
	decrement(s.source, source)
	decrement(s.ranges, ranges)
}

func decrement(held map[bucket]int, b bucket) {
	if held[b] <= 1 {
		delete(held, b)
		return
	}
	held[b]--
}
