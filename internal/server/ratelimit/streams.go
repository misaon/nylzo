package ratelimit

import "sync"

const (
	maxStreamsPerSource = 2
	maxStreamsPerRange  = 8
)

type Streams struct {
	mu        sync.Mutex
	total     int
	maxTotal  int
	perSource map[Bucket]int
	perRange  map[Bucket]int
}

func NewStreams(maxTotal int) *Streams {
	return &Streams{maxTotal: maxTotal, perSource: map[Bucket]int{}, perRange: map[Bucket]int{}}
}

func (s *Streams) Take(source, addressRange Bucket) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	full := s.total >= s.maxTotal || s.perSource[source] >= maxStreamsPerSource ||
		s.perRange[addressRange] >= maxStreamsPerRange
	if full {
		return false
	}
	s.total++
	s.perSource[source]++
	s.perRange[addressRange]++
	return true
}

func (s *Streams) Release(source, addressRange Bucket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total--
	decrement(s.perSource, source)
	decrement(s.perRange, addressRange)
}

func decrement(held map[Bucket]int, bucket Bucket) {
	if held[bucket] <= 1 {
		delete(held, bucket)
		return
	}
	held[bucket]--
}
