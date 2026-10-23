package server

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestClosingStopsLeaseTimersAndWaitsForRunningOnes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		pending := &timers{}
		var ran atomic.Int32
		pending.after(time.Hour, func() { ran.Add(1) })
		release := make(chan struct{})
		pending.after(time.Second, func() {
			<-release
			ran.Add(10)
		})
		time.Sleep(time.Second)
		synctest.Wait()
		stopped := make(chan struct{})
		go func() {
			pending.stop()
			close(stopped)
		}()
		synctest.Wait()
		select {
		case <-stopped:
			t.Fatal("stop returned while a timer was still running")
		default:
		}
		close(release)
		<-stopped
		pending.after(0, func() { ran.Add(100) })
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		if got := ran.Load(); got != 10 {
			t.Fatalf("timers that ran: %d, want only the one already running", got)
		}
	})
}
