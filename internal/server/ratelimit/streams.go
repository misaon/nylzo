package ratelimit

import "sync"

const (
	maxStreamsPerSource = 2
	maxStreamsPerRange  = 4
)

type Streams struct {
	mutex     sync.Mutex
	total     int
	maxTotal  int
	perSource map[Bucket]int
	perRange  map[Bucket]int
}

func NewStreams(maxTotal int) *Streams {
	return &Streams{maxTotal: maxTotal, perSource: map[Bucket]int{}, perRange: map[Bucket]int{}}
}

func (s *Streams) Take(source, addressRange Bucket) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
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
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.total--
	decrement(s.perSource, source)
	decrement(s.perRange, addressRange)
}

// Purge copies the slots still held into new maps, so no released bucket stays in memory.
func (s *Streams) Purge() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	held := func(count int) bool { return count > 0 }
	s.perSource = Rebuilt(s.perSource, held)
	s.perRange = Rebuilt(s.perRange, held)
}

func decrement(held map[Bucket]int, bucket Bucket) {
	if held[bucket] <= 1 {
		delete(held, bucket)
		return
	}
	held[bucket]--
}
