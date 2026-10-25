package diskspace

import (
	"log/slog"
	"sync/atomic"

	"github.com/misaon/nylzo/internal/server/logging"
)

type FreeFunc func(dir string) (uint64, error)

type Reserver struct {
	dir      string
	minFree  int64
	free     FreeFunc
	logger   *slog.Logger
	reserved atomic.Int64
	low      atomic.Bool
}

func NewReserver(dir string, minFree int64, free FreeFunc, logger *slog.Logger) *Reserver {
	return &Reserver{dir: dir, minFree: minFree, free: free, logger: logger}
}

func (r *Reserver) Reserve(size int64) (func(), bool) {
	if r.minFree == 0 {
		return func() {}, true
	}
	free, err := r.free(r.dir)
	reserved := r.reserved.Add(size)
	if err == nil && r.fits(free, reserved) {
		if r.low.Swap(false) {
			r.logger.Info("disk-free-again")
		}
		return func() { r.reserved.Add(-size) }, true
	}
	r.reserved.Add(-size)
	switch {
	case r.low.Swap(true):
	case err != nil:
		r.logger.Error("disk-free", "err", logging.ErrorText(err))
	default:
		r.logger.Error("disk-full")
	}
	return nil, false
}

func (r *Reserver) fits(free uint64, reserved int64) bool {
	if r.minFree < 0 || reserved < 0 {
		return false
	}
	return free >= uint64(r.minFree)+uint64(reserved)
}
