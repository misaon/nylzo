package store

import (
	"context"
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
	ref, err := s.End(ctx, id, state, now)
	if err != nil {
		return err
	}
	if ref == "" {
		return nil
	}
	return b.Delete(ctx, ref)
}

func Reconcile(ctx context.Context, s Store, b Blobs, now time.Time) (orphans int, temp int, err error) {
	known, err := s.KnownBlobRefs(ctx)
	if err != nil {
		return 0, 0, err
	}
	objects, err := b.List(ctx)
	if err != nil {
		return 0, 0, err
	}
	cutoff := now.Add(-ReconcileGrace)
	for _, o := range objects {
		if o.Modified.After(cutoff) {
			continue
		}
		if _, ok := known[o.Ref]; ok {
			continue
		}
		if err := b.Delete(ctx, o.Ref); err != nil {
			return orphans, temp, err
		}
		orphans++
	}
	temp, err = b.PurgeTemp(ctx, cutoff)
	return orphans, temp, err
}

func SweepAll(ctx context.Context, s Store, b Blobs, now time.Time, reconcile bool) (SweepReport, error) {
	report, err := s.Sweep(ctx, now, func(ref string) error { return b.Delete(ctx, ref) })
	if err != nil {
		return report, err
	}
	if reconcile {
		orphans, temp, err := Reconcile(ctx, s, b, now)
		report.OrphanedBlobs = orphans
		report.StaleTempFiles = temp
		if err != nil {
			return report, err
		}
	}
	return report, nil
}
