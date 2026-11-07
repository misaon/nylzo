package sweep

import (
	"context"
	"errors"
	"time"

	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const reconcileKey = "sweep:reconcile:d"

func Run(
	ctx context.Context,
	storage store.Store,
	blobs store.Blobs,
	dataDirectory string,
	now time.Time,
) (store.SweepReport, error) {
	report, err := store.SweepAll(ctx, storage, blobs, now)
	pruneErr := errors.Join(abuse.Prune(dataDirectory, now), abuse.Prepare(dataDirectory, now))
	orphans, reconcileErr := reconcileOncePerDay(ctx, storage, blobs, now)
	report.OrphanedBlobs = orphans
	restampErr := errors.Join(blobs.Restamp(ctx, now), abuse.Restamp(dataDirectory, now))
	return report, errors.Join(err, pruneErr, reconcileErr, restampErr)
}

func reconcileOncePerDay(ctx context.Context, storage store.Store, blobs store.Blobs, now time.Time) (int, error) {
	day := ratelimit.Day(now)
	done, err := storage.RateGet(ctx, reconcileKey, day.Start)
	if err != nil || done.Count > 0 {
		return 0, err
	}
	report, err := store.Reconcile(ctx, storage, blobs, now)
	if !report.Listed {
		return report.Orphans, err
	}
	_, markErr := storage.RateHit(
		ctx,
		[]store.RateHit{{Key: reconcileKey, WindowStart: day.Start, PurgeAt: day.PurgeAt()}},
	)
	return report.Orphans, errors.Join(err, markErr)
}

// FirstError names only the first failure of a sweep; a count of the failed items would count the blobs and rows
// that came due while the storage failed (spec §10).
func FirstError(err error) string {
	failures := logging.JoinedErrors(err)
	if len(failures) == 0 {
		return logging.ErrorText(err)
	}
	return logging.ErrorText(failures[0])
}
