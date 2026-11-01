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
	dataDir string,
	now time.Time,
) (store.SweepReport, error) {
	report, err := store.SweepAll(ctx, storage, blobs, now)
	pruneErr := abuse.Prune(dataDir, now)
	orphans, reconcileErr := reconcileOncePerDay(ctx, storage, blobs, now)
	report.OrphanedBlobs = orphans
	restampErr := errors.Join(blobs.Restamp(ctx, now), abuse.Restamp(dataDir, now))
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

func LogAttributes(report store.SweepReport, err error) []any {
	attributes := report.LogAttrs()
	if err == nil {
		return attributes
	}
	failures := logging.JoinedErrors(err)
	if len(failures) == 0 {
		failures = []error{err}
	}
	return append(attributes, "errors", len(failures), "err", logging.ErrorText(failures[0]))
}
