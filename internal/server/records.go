package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/store"
)

var errSealedBeforeFailure = errors.New(
	"server: stored rows were sealed with the current pepper before the start failed; releases before 0.14.0 no " +
		"longer open them",
)

// SealRows puts the wraps of every stored row under the current pepper before the server takes a request: the rows of
// an earlier release, which have no pepper layer, and the rows that the previous pepper sealed (spec §5.1, §5.2).
func (s *Server) SealRows(ctx context.Context) error {
	var earlier []seal.Key
	secretmode.Do(func() {
		if previous, ok := s.ring.PreviousRecordsKey(); ok {
			earlier = []seal.Key{previous}
		}
	})
	defer clear(earlier)
	defer s.ring.ForgetPreviousRecordsKey()
	var resealed, unopenable bool
	err := s.store.ResealWraps(ctx, func(row *store.Row) bool {
		var changed bool
		var resealErr error
		secretmode.Do(func() {
			changed, resealErr = store.ResealRow(row, s.recordsKey, earlier)
		})
		if row.Locator != store.DummyLocator {
			resealed = resealed || changed
			unopenable = unopenable || resealErr != nil
		}
		return changed
	})
	if err != nil && resealed {
		return fmt.Errorf("%w: %w", errSealedBeforeFailure, err)
	}
	if err != nil {
		return err
	}
	s.logResealing(ctx, resealed, unopenable)
	return nil
}

func (s *Server) logResealing(ctx context.Context, resealed, unopenable bool) {
	if resealed {
		s.logger.InfoContext(
			ctx,
			"rows-sealed",
			"notice", "the wraps of stored rows are now sealed with the current pepper",
		)
	}
	if unopenable {
		s.logger.ErrorContext(
			ctx,
			"rows-unopenable",
			"notice", "no configured pepper opens some stored rows; check NYLZO_PEPPER_FILE, or set "+
				"NYLZO_PEPPER_PREVIOUS_FILE to the pepper that sealed them; otherwise they stay closed until the sweep "+
				"purges them",
		)
	}
}
