package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type SweepReport struct {
	Expired        int
	ExpiredLeases  int
	BlobsReleased  int
	Tombstones     int
	Counters       int
	Resumed        int
	OrphanedBlobs  int
	StaleTempFiles int
}

func (r SweepReport) LogAttrs() []any {
	return []any{
		"expired", r.Expired,
		"leases", r.ExpiredLeases,
		"released", r.BlobsReleased,
		"tombstones", r.Tombstones,
		"counters", r.Counters,
		"resumed", r.Resumed,
		"orphans", r.OrphanedBlobs,
		"temp", r.StaleTempFiles,
	}
}

func SweepAll(ctx context.Context, storage Store, blobs Blobs, now time.Time) (SweepReport, error) {
	var report SweepReport
	var errs []error
	count := func(total *int, step func() (int, error)) {
		done, err := step()
		*total = done
		errs = append(errs, err)
	}
	count(&report.Expired, func() (int, error) { return sweepExpired(ctx, storage, blobs, now) })
	count(&report.ExpiredLeases, func() (int, error) { return purge(ctx, "leases", storage.PurgeLeases, now) })
	count(&report.BlobsReleased, func() (int, error) {
		spent := func(after string) ([]string, error) { return storage.SpentPage(ctx, after, SweepBatch) }
		release := func(id string) (bool, error) { return Release(ctx, storage, blobs, id, now) }
		return eachPage(ctx, "release", spent, release)
	})
	count(&report.Resumed, func() (int, error) {
		ending := func(after string) ([]string, error) { return storage.EndingPage(ctx, now, after, SweepBatch) }
		finish := func(id string) (bool, error) {
			_, err := EndMessage(ctx, storage, blobs, id, StateDeleted, now)
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			return err == nil, err
		}
		return eachPage(ctx, "resume ending", ending, finish)
	})
	count(&report.Tombstones, func() (int, error) { return purge(ctx, "tombstones", storage.PurgeTombstones, now) })
	count(&report.Counters, func() (int, error) { return purge(ctx, "counters", storage.RatePurge, now) })
	errs = append(errs, storage.Checkpoint(ctx))
	count(&report.StaleTempFiles, func() (int, error) { return blobs.PurgeTemp(ctx, now.Add(-BlobGrace)) })
	return report, errors.Join(errs...)
}

func sweepExpired(ctx context.Context, messages Messages, blobs Blobs, now time.Time) (int, error) {
	var errs []error
	ended := 0
	var after ExpiredMessage
	for ctx.Err() == nil {
		page, err := messages.ExpiredPage(ctx, now, after, SweepBatch)
		if err != nil {
			return ended, errors.Join(append(errs, fmt.Errorf("store: list expired: %w", err))...)
		}
		endedOnPage, err := endExpiredPage(context.WithoutCancel(ctx), messages, blobs, page, now)
		ended += endedOnPage
		errs = append(errs, err)
		if len(page) < SweepBatch {
			break
		}
		after = page[len(page)-1]
	}
	return ended, errors.Join(errs...)
}

func endExpiredPage(
	ctx context.Context,
	messages Messages,
	blobs Blobs,
	page []ExpiredMessage,
	now time.Time,
) (int, error) {
	refs := make([]string, 0, len(page))
	for _, expired := range page {
		if expired.BlobRef != "" {
			refs = append(refs, expired.BlobRef)
		}
	}
	var deleteErrs []error
	if len(refs) > 0 {
		deleteErrs = blobs.DeleteBatch(ctx, refs)
	}
	var errs []error
	ids := make([]string, 0, len(page))
	for _, expired := range page {
		if expired.BlobRef != "" {
			deleteErr := deleteErrs[0]
			deleteErrs = deleteErrs[1:]
			if deleteErr != nil {
				errs = append(errs, fmt.Errorf("store: end expired: %w", deleteErr))
				continue
			}
		}
		ids = append(ids, expired.ID)
	}
	if len(ids) == 0 {
		return 0, errors.Join(errs...)
	}
	ended, err := messages.EndExpired(ctx, ids, now)
	return ended, errors.Join(append(errs, err)...)
}

func eachPage(
	ctx context.Context,
	step string,
	page func(after string) ([]string, error),
	handle func(id string) (bool, error),
) (int, error) {
	var errs []error
	done := 0
	after := ""
	for ctx.Err() == nil {
		ids, err := page(after)
		if err != nil {
			return done, errors.Join(append(errs, fmt.Errorf("store: %s: %w", step, err))...)
		}
		handled, pageErrs := handleAll(ctx, step, ids, handle)
		done += handled
		errs = append(errs, pageErrs...)
		if len(ids) < SweepBatch {
			break
		}
		after = ids[len(ids)-1]
	}
	return done, errors.Join(errs...)
}

func handleAll(ctx context.Context, step string, ids []string, handle func(id string) (bool, error)) (int, []error) {
	done := 0
	var errs []error
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		handled, err := handle(id)
		if err != nil {
			errs = append(errs, fmt.Errorf("store: %s: %w", step, err))
			continue
		}
		if handled {
			done++
		}
	}
	return done, errs
}

func purge(
	ctx context.Context,
	step string,
	page func(context.Context, time.Time, int) (int, error),
	now time.Time,
) (int, error) {
	total := 0
	for {
		purged, err := page(ctx, now, SweepBatch)
		if err != nil {
			return total, fmt.Errorf("store: purge %s: %w", step, err)
		}
		total += purged
		if purged < SweepBatch {
			return total, nil
		}
	}
}
