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
		"expired",
		r.Expired,
		"leases",
		r.ExpiredLeases,
		"released",
		r.BlobsReleased,
		"tombstones",
		r.Tombstones,
		"counters",
		r.Counters,
		"resumed",
		r.Resumed,
		"orphans",
		r.OrphanedBlobs,
		"temp",
		r.StaleTempFiles,
	}
}

func SweepAll(ctx context.Context, s Store, b Blobs, now time.Time) (SweepReport, error) {
	var r SweepReport
	var err error
	r.Expired, err = sweepExpired(ctx, s, b, now)
	errs := []error{err}
	r.ExpiredLeases, err = purge(ctx, "leases", s.PurgeLeases, now)
	errs = append(errs, err)
	r.BlobsReleased, err = eachPage(ctx, "release", func(after string) ([]string, error) {
		return s.SpentPage(ctx, after, SweepBatch)
	}, func(id string) (bool, error) {
		return Release(ctx, s, b, id, now)
	})
	errs = append(errs, err)
	r.Resumed, err = eachPage(ctx, "resume ending", func(after string) ([]string, error) {
		return s.EndingPage(ctx, now, after, SweepBatch)
	}, func(id string) (bool, error) {
		_, err := EndMessage(ctx, s, b, id, StateDeleted, now)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	})
	errs = append(errs, err)
	r.Tombstones, err = purge(ctx, "tombstones", s.PurgeTombstones, now)
	errs = append(errs, err)
	r.Counters, err = purge(ctx, "counters", s.RatePurge, now)
	errs = append(errs, err, s.Checkpoint(ctx))
	r.StaleTempFiles, err = b.PurgeTemp(ctx, now.Add(-BlobGrace))
	return r, errors.Join(append(errs, err)...)
}

func sweepExpired(ctx context.Context, s Messages, b Blobs, now time.Time) (int, error) {
	var errs []error
	ended := 0
	var after ExpiredMessage
	for ctx.Err() == nil {
		page, err := s.ExpiredPage(ctx, now, after, SweepBatch)
		if err != nil {
			return ended, errors.Join(append(errs, fmt.Errorf("store: list expired: %w", err))...)
		}
		n, err := endExpiredPage(context.WithoutCancel(ctx), s, b, page, now)
		ended += n
		errs = append(errs, err)
		if len(page) < SweepBatch {
			break
		}
		after = page[len(page)-1]
	}
	return ended, errors.Join(errs...)
}

func endExpiredPage(ctx context.Context, s Messages, b Blobs, page []ExpiredMessage, now time.Time) (int, error) {
	refs := make([]string, 0, len(page))
	for _, m := range page {
		if m.BlobRef != "" {
			refs = append(refs, m.BlobRef)
		}
	}
	var deleteErrs []error
	if len(refs) > 0 {
		deleteErrs = b.DeleteBatch(ctx, refs)
	}
	var errs []error
	ids := make([]string, 0, len(page))
	for _, m := range page {
		if m.BlobRef != "" {
			err := deleteErrs[0]
			deleteErrs = deleteErrs[1:]
			if err != nil {
				errs = append(errs, fmt.Errorf("store: end expired: %w", err))
				continue
			}
		}
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		return 0, errors.Join(errs...)
	}
	n, err := s.EndExpired(ctx, ids, now)
	return n, errors.Join(append(errs, err)...)
}

func eachPage(
	ctx context.Context,
	step string,
	page func(after string) ([]string, error),
	fn func(id string) (bool, error),
) (int, error) {
	var errs []error
	done := 0
	after := ""
	for ctx.Err() == nil {
		ids, err := page(after)
		if err != nil {
			return done, errors.Join(append(errs, fmt.Errorf("store: %s: %w", step, err))...)
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				break
			}
			ok, err := fn(id)
			if err != nil {
				errs = append(errs, fmt.Errorf("store: %s: %w", step, err))
				continue
			}
			if ok {
				done++
			}
		}
		if len(ids) < SweepBatch {
			break
		}
		after = ids[len(ids)-1]
	}
	return done, errors.Join(errs...)
}

func purge(
	ctx context.Context,
	step string,
	page func(context.Context, time.Time, int) (int, error),
	now time.Time,
) (int, error) {
	total := 0
	for {
		n, err := page(ctx, now, SweepBatch)
		if err != nil {
			return total, fmt.Errorf("store: purge %s: %w", step, err)
		}
		total += n
		if n < SweepBatch {
			return total, nil
		}
	}
}
