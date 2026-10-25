package store

import (
	"context"
	"errors"
	"time"
)

func Release(ctx context.Context, messages Messages, blobs Blobs, id string, now time.Time) (bool, error) {
	ref, err := messages.ReleaseIfDone(ctx, id, now)
	if err != nil || ref == "" {
		return false, err
	}
	return true, deleteReleasedBlob(ctx, messages, blobs, id, ref, now)
}

//nolint:gocritic // hugeParam: internal/server passes the lease by value; see the cross-area list.
func AckLease(ctx context.Context, messages Messages, blobs Blobs, lease Lease, now time.Time) error {
	ref, err := messages.Ack(ctx, lease, now)
	if err != nil || ref == "" {
		return err
	}
	return deleteReleasedBlob(ctx, messages, blobs, lease.ID, ref, now)
}

func EndMessage(
	ctx context.Context,
	messages Messages,
	blobs Blobs,
	id string,
	state State,
	now time.Time,
) (State, error) {
	ctx = context.WithoutCancel(ctx)
	ending, ref, recorded, err := messages.MarkEnding(ctx, id, state, now)
	if err != nil {
		return "", err
	}
	if ref != "" {
		err = blobs.Delete(ctx, ref)
		if err != nil {
			return ending, err
		}
	}
	err = messages.End(ctx, id, ending, now)
	if recorded && errors.Is(err, ErrNotFound) {
		return ending, nil
	}
	return ending, err
}

//nolint:gocritic // unnamedResult: internal/server calls this signature; see the cross-area list.
func Reconcile(ctx context.Context, messages Messages, blobs Blobs, now time.Time) (int, bool, error) {
	known, err := messages.KnownBlobRefs(ctx)
	if err != nil {
		return 0, false, err
	}
	objects, err := blobs.List(ctx)
	if err != nil {
		return 0, false, err
	}
	cutoff := now.Add(-BlobGrace)
	orphans := 0
	var errs []error
	for _, object := range objects {
		if ctx.Err() != nil {
			break
		}
		if _, live := known[object.Ref]; live || object.Modified.After(cutoff) {
			continue
		}
		if err = blobs.Delete(ctx, object.Ref); err != nil {
			errs = append(errs, err)
			continue
		}
		orphans++
	}
	return orphans, true, errors.Join(errs...)
}

func deleteReleasedBlob(ctx context.Context, messages Messages, blobs Blobs, id, ref string, now time.Time) error {
	ctx = context.WithoutCancel(ctx)
	if err := blobs.Delete(ctx, ref); err != nil {
		return err
	}
	return messages.ClearBlob(ctx, id, ref, now)
}
