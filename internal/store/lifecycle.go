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
	if err := b.Delete(ctx, ref); err != nil {
		return false, err
	}
	return true, s.ClearBlob(ctx, id, ref, now)
}

func EndMessage(ctx context.Context, s Store, b Blobs, id string, state State, now time.Time) error {
	ctx = context.WithoutCancel(ctx)
	rec, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if rec.Message == nil {
		return ErrNotFound
	}
	if ref := rec.Message.BlobRef; ref != "" {
		if err := b.Delete(ctx, ref); err != nil {
			return err
		}
	}
	return s.End(ctx, id, state, now)
}

func Reconcile(ctx context.Context, s Store, b Blobs, now time.Time) (int, error) {
	known, err := s.KnownBlobRefs(ctx)
	if err != nil {
		return 0, err
	}
	objects, err := b.List(ctx)
	if err != nil {
		return 0, err
	}
	cutoff := now.Add(-ReconcileGrace)
	orphans := 0
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
	return orphans, errors.Join(errs...)
}

func SweepAll(ctx context.Context, s Store, b Blobs, now time.Time) (SweepReport, error) {
	report, err := s.Sweep(ctx, now, func(ref string) error { return b.Delete(ctx, ref) })
	temp, terr := b.PurgeTemp(ctx, now.Add(-ReconcileGrace))
	report.StaleTempFiles = temp
	return report, errors.Join(err, terr)
}
