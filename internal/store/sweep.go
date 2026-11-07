package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

type SweepReport struct {
	Blobs          int
	Rows           int
	Counters       int
	OrphanedBlobs  int
	StaleTempFiles int
}

// SweepAll never opens a record: it deletes the blobs and rows whose cleanup time has passed and replenishes the pool
// of files that uploads take.
func SweepAll(ctx context.Context, storage Store, blobs Blobs, now time.Time) (SweepReport, error) {
	var report SweepReport
	var errs []error
	count := func(total *int, step func() (int, error)) {
		done, err := step()
		*total = done
		errs = append(errs, err)
	}
	count(&report.Blobs, func() (int, error) { return deleteDueBlobs(ctx, storage, blobs, now) })
	count(&report.Rows, func() (int, error) { return purge(ctx, "rows", storage.PurgeRows, now, SweepBatch) })
	count(&report.Counters, func() (int, error) {
		return purge(ctx, "counters", storage.RatePurge, now, CounterPurgeBatch)
	})
	errs = append(errs, storage.Checkpoint(ctx))
	count(&report.StaleTempFiles, func() (int, error) { return blobs.PurgeTemp(ctx, now.Add(-BlobGrace)) })
	errs = append(errs, blobs.Replenish(ctx))
	return report, errors.Join(errs...)
}

func deleteDueBlobs(ctx context.Context, messages Messages, blobs Blobs, now time.Time) (int, error) {
	var errs []error
	deleted := 0
	var after seal.Locator
	for ctx.Err() == nil {
		page, err := messages.DueBlobs(ctx, now, after, SweepBatch)
		if err != nil {
			return deleted, errors.Join(append(errs, fmt.Errorf("store: list due blobs: %w", err))...)
		}
		deletedOnPage, err := deleteBlobPage(context.WithoutCancel(ctx), messages, blobs, page)
		deleted += deletedOnPage
		errs = append(errs, err)
		if len(page) < SweepBatch {
			break
		}
		after = page[len(page)-1].Locator
	}
	return deleted, errors.Join(errs...)
}

func deleteBlobPage(ctx context.Context, messages Messages, blobs Blobs, page []DueBlob) (int, error) {
	if len(page) == 0 {
		return 0, nil
	}
	if err := messages.ShredBlobs(ctx, page); err != nil {
		return 0, fmt.Errorf("store: shred blobs: %w", err)
	}
	refs := make([]string, len(page))
	for i, due := range page {
		refs[i] = due.Ref
	}
	var errs []error
	deleted := make([]DueBlob, 0, len(page))
	for i, err := range blobs.DeleteBatch(ctx, refs) {
		if err != nil {
			errs = append(errs, fmt.Errorf("store: delete blob: %w", err))
			continue
		}
		deleted = append(deleted, page[i])
	}
	if len(deleted) > 0 {
		if err := messages.ForgetBlobs(ctx, deleted); err != nil {
			return 0, errors.Join(append(errs, fmt.Errorf("store: forget blobs: %w", err))...)
		}
	}
	return len(deleted), errors.Join(errs...)
}

func purge(
	ctx context.Context,
	step string,
	page func(context.Context, time.Time, int) (int, error),
	now time.Time,
	batch int,
) (int, error) {
	total := 0
	for {
		purged, err := page(ctx, now, batch)
		if err != nil {
			return total, fmt.Errorf("store: purge %s: %w", step, err)
		}
		total += purged
		if purged < batch {
			return total, nil
		}
	}
}
