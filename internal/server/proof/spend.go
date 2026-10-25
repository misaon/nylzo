package proof

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	maxSeenNonces = 1 << 16
	maxSpendBatch = 256
)

var errSpendAborted = errors.New("server: the challenge spend write stopped before it finished")

type seenNonces struct {
	mutex      sync.Mutex
	maxEntries int
	expires    map[nonce]int64
}

func newSeenNonces() *seenNonces {
	return &seenNonces{maxEntries: maxSeenNonces, expires: map[nonce]int64{}}
}

func (s *seenNonces) remember(spent nonce, expires, now int64) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, seen := s.expires[spent]; seen {
		return true
	}
	if len(s.expires) >= s.maxEntries {
		ratelimit.Trim(s.expires, s.maxEntries, func(expiry int64) bool {
			return now > expiry+int64(clockSkew/time.Second)
		})
	}
	s.expires[spent] = expires
	return false
}

type spendBatch struct {
	hits     []store.RateHit
	turn     chan struct{}
	done     chan struct{}
	reserved []bool
	err      error
}

// spendBatcher writes one batch at a time; spends that arrive meanwhile wait and share the next transaction.
type spendBatcher struct {
	counters store.Counters
	maxSize  int
	mutex    sync.Mutex
	writing  bool
	queue    []*spendBatch
}

func newSpendBatcher(counters store.Counters) *spendBatcher {
	return &spendBatcher{counters: counters, maxSize: maxSpendBatch}
}

func (b *spendBatcher) reserve(ctx context.Context, hit *store.RateHit) (bool, error) {
	batch, index, leads := b.join(hit)
	if leads {
		<-batch.turn
		b.write(ctx, batch)
	}
	select {
	case <-batch.done:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if batch.err != nil {
		return false, batch.err
	}
	return batch.reserved[index], nil
}

func (b *spendBatcher) join(hit *store.RateHit) (*spendBatch, int, bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if queued := len(b.queue); queued == 0 || len(b.queue[queued-1].hits) == b.maxSize {
		b.queue = append(b.queue, &spendBatch{turn: make(chan struct{}), done: make(chan struct{})})
	}
	batch := b.queue[len(b.queue)-1]
	index := len(batch.hits)
	batch.hits = append(batch.hits, *hit)
	if index == 0 && !b.writing {
		b.writing = true
		close(batch.turn)
	}
	return batch, index, index == 0
}

func (b *spendBatcher) write(ctx context.Context, batch *spendBatch) {
	b.mutex.Lock()
	b.queue[0] = nil
	b.queue = b.queue[1:]
	b.mutex.Unlock()
	defer b.passTurn()
	defer close(batch.done)
	defer func() {
		if recover() != nil {
			batch.reserved, batch.err = nil, errSpendAborted
		}
	}()
	batch.reserved, batch.err = b.counters.RateReserveBatch(context.WithoutCancel(ctx), batch.hits)
}

func (b *spendBatcher) passTurn() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.queue) > 0 {
		close(b.queue[0].turn)
		return
	}
	b.writing = false
}
