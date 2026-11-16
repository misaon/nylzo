package hostusage

import (
	"context"
	"runtime/metrics"
	"sync"
	"time"
)

const (
	unknownShare = -1.0

	totalMemoryMetric    = "/memory/classes/total:bytes"
	releasedMemoryMetric = "/memory/classes/heap/released:bytes"
)

type Sample struct {
	At           time.Time
	Processor    float64
	Load         float64
	MemoryUsed   uint64
	MemoryTotal  uint64
	ProcessBytes uint64
}

type machineReading struct {
	Processor   float64
	Load        float64
	MemoryUsed  uint64
	MemoryTotal uint64
}

func unknownReading() machineReading {
	return machineReading{Processor: unknownShare, Load: unknownShare}
}

type Sampler struct {
	mutex  sync.Mutex
	reader *machineReader
	ring   []Sample
	oldest int
	count  int
}

func NewSampler(capacity int) *Sampler {
	return &Sampler{reader: newMachineReader(), ring: make([]Sample, max(capacity, 1))}
}

func Supported() bool {
	return supported
}

func (s *Sampler) Run(ctx context.Context, interval time.Duration) {
	s.take(time.Now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.take(now)
		}
	}
}

func (s *Sampler) History() []Sample {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	history := make([]Sample, s.count)
	for i := range history {
		history[i] = s.ring[(s.oldest+i)%len(s.ring)]
	}
	return history
}

func (s *Sampler) take(now time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	reading, _ := s.reader.read()
	sample := Sample{
		At:           now,
		Processor:    reading.Processor,
		Load:         reading.Load,
		MemoryUsed:   reading.MemoryUsed,
		MemoryTotal:  reading.MemoryTotal,
		ProcessBytes: processBytes(),
	}
	if s.count < len(s.ring) {
		s.ring[(s.oldest+s.count)%len(s.ring)] = sample
		s.count++
		return
	}
	s.ring[s.oldest] = sample
	s.oldest = (s.oldest + 1) % len(s.ring)
}

func processBytes() uint64 {
	samples := []metrics.Sample{{Name: totalMemoryMetric}, {Name: releasedMemoryMetric}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindUint64 || samples[1].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	total, released := samples[0].Value.Uint64(), samples[1].Value.Uint64()
	if released > total {
		return 0
	}
	return total - released
}

type Disk struct {
	FreeBytes  uint64
	TotalBytes uint64
}
