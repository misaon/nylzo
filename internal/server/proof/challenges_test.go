package proof

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

func spendHit(key string) store.RateHit {
	return store.RateHit{
		Key:         challengeSpentKey + key,
		WindowStart: time.Unix(0, 0),
		PurgeAt:     time.Unix(4102444800, 0),
		Limit:       1,
	}
}

type failingSpends struct {
	store.Store

	fail atomic.Bool
}

func (s *failingSpends) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	if s.fail.Load() {
		return nil, errors.New("test: the store is down")
	}
	return s.Store.RateReserveBatch(ctx, hits)
}

type panicOnce struct {
	store.Store

	panicked atomic.Bool
}

func (p *panicOnce) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	if !p.panicked.Swap(true) {
		panic("test: the store driver panicked")
	}
	return p.Store.RateReserveBatch(ctx, hits)
}

type gatedSpends struct {
	store.Store

	gate    chan struct{}
	mu      sync.Mutex
	batches []int
}

func (s *gatedSpends) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	s.mu.Lock()
	first := len(s.batches) == 0
	s.batches = append(s.batches, len(hits))
	s.mu.Unlock()
	if first {
		<-s.gate
	}
	return s.Store.RateReserveBatch(ctx, hits)
}

func (s *gatedSpends) calls() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.batches)
}

func TestChallengesAreSpentOnceWithinTheirWindow(t *testing.T) {
	t.Parallel()
	challenges := NewChallenges(testRing(t), openStore(t))
	now := time.Now()
	for _, c := range []struct {
		issued time.Time
		spent  bool
	}{
		{now, true},
		{now.Add(5 * time.Second), true},
		{now.Add(6 * time.Second), false},
		{now.Add(-challengeTTL - time.Second), false},
	} {
		challenge, _ := challenges.Issue(c.issued)
		if spent, err := challenges.Spend(t.Context(), challenge, now); err != nil || spent != c.spent {
			t.Fatalf("a challenge issued %v from now: spent %v %v", c.issued.Sub(now), spent, err)
		}
	}
	challenge, _ := challenges.Issue(now)
	challenge[len(challenge)-1] ^= 1
	if spent, err := challenges.Spend(t.Context(), challenge, now); err != nil || spent {
		t.Fatalf("a challenge with a wrong tag was spent: %v", err)
	}
	other := NewChallenges(testRing(t), challenges.batches.counters)
	repeated, _ := challenges.Issue(now)
	if spent, err := challenges.Spend(t.Context(), repeated, now); err != nil || !spent {
		t.Fatalf("first spend: %v", err)
	}
	if spent, err := other.Spend(t.Context(), repeated, now); err != nil || spent {
		t.Fatalf("another instance spent the same challenge again: %v", err)
	}
}

func TestAFailedSpendRefusesTheChallenge(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	failing := &failingSpends{Store: st}
	failing.fail.Store(true)
	challenges := NewChallenges(testRing(t), failing)
	now := time.Now()
	challenge, _ := challenges.Issue(now)
	if _, err := challenges.Spend(t.Context(), challenge, now); err == nil {
		t.Fatal("a spend the store could not record succeeded")
	}
	failing.fail.Store(false)
	if spent, err := challenges.Spend(t.Context(), challenge, now); err != nil || spent {
		t.Fatalf("the same process accepted the challenge after a failed spend: %v %v", spent, err)
	}
	key := challengeSpentKey + hex.EncodeToString(challenge[4:challengeSignedEnd])
	if counter, err := st.RateGet(t.Context(), key, time.Unix(0, 0)); err != nil || counter.Count != 0 {
		t.Fatal("the repeat reached the store although the process had seen the nonce")
	}
}

func TestSeenNoncesStayBounded(t *testing.T) {
	t.Parallel()
	seen := newSeenNonces()
	seen.maxEntries = 64
	for i := range seen.maxEntries {
		seen.remember(nonce{byte(i)}, 100, 100)
	}
	if !seen.remember(nonce{1}, 100, 100) {
		t.Fatal("a remembered nonce was not reported as seen")
	}
	seen.remember(nonce{0xff, 1}, 1000, 1000)
	if len(seen.expires) != 1 {
		t.Fatalf("expired nonces were kept: %d", len(seen.expires))
	}
	for i := range 4 * seen.maxEntries {
		seen.remember(nonce{byte(i), byte(i >> 8), 2}, 2000, 1000)
	}
	if len(seen.expires) > seen.maxEntries {
		t.Fatalf("the nonce memory grew past its bound: %d", len(seen.expires))
	}
}

func TestASpendPanicFailsOnlyItsBatch(t *testing.T) {
	t.Parallel()
	batcher := newSpendBatcher(&panicOnce{Store: openStore(t)})
	if _, err := batcher.reserve(t.Context(), new(spendHit("a"))); !errors.Is(err, errSpendAborted) {
		t.Fatalf("a batch whose write panicked: %v", err)
	}
	if _, err := batcher.reserve(t.Context(), new(spendHit("b"))); err != nil {
		t.Fatalf("the next batch failed: %v", err)
	}
}

func TestConcurrentSpendsShareATransaction(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		gated := &gatedSpends{Store: openStore(t), gate: make(chan struct{})}
		batcher := newSpendBatcher(gated)
		var spends sync.WaitGroup
		var granted atomic.Int64
		reserve := func(key string) {
			spends.Go(func() {
				reserved, err := batcher.reserve(t.Context(), new(spendHit(key)))
				if err != nil {
					t.Error(err)
				}
				if reserved {
					granted.Add(1)
				}
			})
		}
		reserve("first")
		synctest.Wait()
		if calls := gated.calls(); len(calls) != 1 {
			t.Fatalf("the first spend did not start writing: %v", calls)
		}
		const pairs = 300
		for i := range 2 * pairs {
			reserve(strconv.Itoa(i % pairs))
		}
		synctest.Wait()
		close(gated.gate)
		spends.Wait()
		if granted.Load() != pairs+1 {
			t.Fatalf("%d spends succeeded, want one per distinct nonce (%d)", granted.Load(), pairs+1)
		}
		want := []int{1, maxSpendBatch, maxSpendBatch, 2*pairs - 2*maxSpendBatch}
		if calls := gated.calls(); !slices.Equal(calls, want) {
			t.Fatalf("spends written as transactions of %v", calls)
		}
	})
}

func TestSpendWaitersStopWhenTheirClientLeaves(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		gated := &gatedSpends{Store: openStore(t), gate: make(chan struct{})}
		batcher := newSpendBatcher(gated)
		go batcher.reserve(t.Context(), new(spendHit("writing")))
		synctest.Wait()
		go batcher.reserve(t.Context(), new(spendHit("next")))
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)
		go func() {
			_, err := batcher.reserve(ctx, new(spendHit("follower")))
			result <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("a waiter whose client left: %v", err)
			}
		default:
			t.Fatal("a waiter whose client left kept waiting for a stalled write")
		}
		close(gated.gate)
		synctest.Wait()
		counter, err := gated.RateGet(t.Context(), challengeSpentKey+"follower", time.Unix(0, 0))
		if err != nil || counter.Count != 1 {
			t.Fatalf("the spend of a client that left was not written: %+v %v", counter, err)
		}
	})
}
