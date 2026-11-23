package hostusage

import (
	"context"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

func TestSamplerKeepsTheNewestSamplesInOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const capacity = 3
		sampler := NewSampler(capacity)
		if kept := sampler.History(); len(kept) != 0 {
			t.Fatal("an empty sampler has a sample")
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			sampler.Run(ctx, time.Second)
			close(done)
		}()
		time.Sleep(4*time.Second + time.Millisecond)
		cancel()
		<-done
		history := sampler.History()
		if len(history) != capacity {
			t.Fatalf("kept %d samples, want %d", len(history), capacity)
		}
		for i := 1; i < len(history); i++ {
			if !history[i].At.After(history[i-1].At) {
				t.Fatalf("samples out of order at %d", i)
			}
		}
		history[0].MemoryTotal = 1
		if sampler.History()[0].MemoryTotal == 1 {
			t.Fatal("History returned the internal buffer")
		}
	})
}

func TestSampleOnThisPlatform(t *testing.T) {
	sampler := NewSampler(1)
	sampler.take(time.Now())
	history := sampler.History()
	if len(history) != 1 {
		t.Fatal("no sample taken")
	}
	sample := history[0]
	if sample.ProcessBytes == 0 {
		t.Error("no process bytes")
	}
	if Supported() && sample.MemoryTotal == 0 {
		t.Fatal("memory total unknown")
	}
	if Supported() && sample.MemoryUsed > sample.MemoryTotal {
		t.Fatalf("used %d above total %d", sample.MemoryUsed, sample.MemoryTotal)
	}
}

func TestProcessBytesAreUnknownWithoutTheirMetrics(t *testing.T) {
	if held := heldBytes("/memory/classes/unknown:bytes", releasedMemoryMetric); held != 0 {
		t.Errorf("a metric this runtime lacks gave %d bytes", held)
	}
	if held := heldBytes(releasedMemoryMetric, totalMemoryMetric); held != 0 {
		t.Errorf("more released than in total gave %d bytes", held)
	}
}

func TestDiskOfAMissingDirectoryFails(t *testing.T) {
	if _, err := DiskOf(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing directory has a disk")
	}
}

func TestDiskOfTemporaryDirectory(t *testing.T) {
	disk, err := DiskOf(t.TempDir())
	if err != nil {
		t.Skipf("disk usage unavailable: %v", err)
	}
	if disk.TotalBytes == 0 || disk.FreeBytes > disk.TotalBytes {
		t.Fatalf("disk %+v", disk)
	}
}
