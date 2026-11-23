package leasetimer

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

func TestClosingStopsLeaseTimersAndWaitsForRunningOnes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		pending := New()
		var ran atomic.Int32
		locator := func(name string) seal.Locator { return seal.LocatorOf([]byte(name)) }
		pending.Schedule(locator("hour"), seal.NewKey(), time.Hour, func(seal.Key) { ran.Add(1) })
		release := make(chan struct{})
		pending.Schedule(locator("second"), seal.NewKey(), time.Second, func(seal.Key) {
			<-release
			ran.Add(10)
		})
		cancelled := pending.pending[locator("hour")]
		pending.Schedule(locator("ended"), seal.NewKey(), time.Second, func(seal.Key) { ran.Add(1000) })
		ended := pending.pending[locator("ended")]
		pending.Cancel(locator("ended"))
		if ended.key != (seal.Key{}) {
			t.Fatal("a release cancelled by an ending must clear its record key at once")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		stopped := make(chan struct{})
		go func() {
			pending.Stop()
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
		pending.Schedule(locator("late"), seal.NewKey(), 0, func(seal.Key) { ran.Add(100) })
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		if got := ran.Load(); got != 10 || cancelled.key != (seal.Key{}) {
			t.Fatalf("timers that ran: %d, want only the one already running, and stop clears the keys", got)
		}
	})
}

func TestAReleaseThatLostTheLockToAReplacementDoesNothing(t *testing.T) {
	t.Parallel()
	timers := New()
	locator := seal.LocatorOf([]byte("message"))
	timers.Schedule(locator, seal.NewKey(), time.Hour, func(seal.Key) { t.Error("the current release ran") })
	current := timers.pending[locator]
	timers.fire(locator, &pendingRelease{key: seal.NewKey()}, func(seal.Key) { t.Error("a replaced release ran") })
	if timers.pending[locator] != current {
		t.Fatal("a replaced release dropped the current one")
	}
	timers.Stop()
	timers.fire(locator, current, func(seal.Key) { t.Error("a release ran after Stop") })
}
