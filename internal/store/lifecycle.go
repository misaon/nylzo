package store

import (
	"context"
	"errors"
	"time"
)

func Release(ctx context.Context, s Store, b Blobs, id string, now time.Time) (bool, error) {
	ref, err := s.ReleaseIfDone(ctx, id, now)
	if err != nil || ref == "" {
		return false, err
	}
	return true, clearReleased(ctx, s, b, id, ref, now)
}

func AckLease(ctx context.Context, s Store, b Blobs, id string, leaseRowID int64, now time.Time) error {
	ref, err := s.Ack(ctx, leaseRowID, now)
	if err != nil || ref == "" {
		return err
	}
	return clearReleased(ctx, s, b, id, ref, now)
}

func clearReleased(ctx context.Context, s Store, b Blobs, id, ref string, now time.Time) error {
	if err := b.Delete(ctx, ref); err != nil {
		return err
	}
	return s.ClearBlob(ctx, id, ref, now)
}

func EndMessage(ctx context.Context, s Store, b Blobs, id string, state State, now time.Time) (State, error) {
	ctx = context.WithoutCancel(ctx)
	ending, ref, err := s.MarkEnding(ctx, id, state, now)
	if err != nil {
		return "", err
	}
	if ref != "" {
		if err := b.Delete(ctx, ref); err != nil {
			return ending, err
		}
	}
	return ending, s.End(ctx, id, ending, now)
}

func Reconcile(ctx context.Context, s Store, b Blobs, now time.Time) (orphans int, listed bool, err error) {
	known, err := s.KnownBlobRefs(ctx)
	if err != nil {
		return 0, false, err
	}
	objects, err := b.List(ctx)
	if err != nil {
		return 0, false, err
	}
	cutoff := now.Add(-BlobGrace)
	var errs []error
	for _, o := range objects {
		if o.Modified.After(cutoff) {
			continue
		}
		if _, ok := known[o.Ref]; ok {
			continue
		}
		if err := b.Delete(ctx, o.Ref); err != nil {
			errs = append(errs, err)
			continue
		}
		orphans++
	}
	return orphans, true, errors.Join(errs...)
}

func SweepAll(ctx context.Context, s Store, b Blobs, now time.Time) (SweepReport, error) {
	report, err := s.Sweep(ctx, now, b)
	temp, terr := b.PurgeTemp(ctx, now.Add(-BlobGrace))
	report.StaleTempFiles = temp
	return report, errors.Join(err, terr)
}
