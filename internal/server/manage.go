package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/statistics"
	"github.com/misaon/nylzo/internal/store"
)

// handleBurn deletes a message only after a handshake whose KE3 matches, checked and counted as for a claim.
func (s *Server) handleBurn(w http.ResponseWriter, r *http.Request) {
	ke3, ok := parseKE3(w, r)
	if !ok {
		return
	}
	granted, ok := s.authorizedOpenable(w, r, seal.Read)
	defer granted.forget()
	if !ok || !s.finishHandshake(w, r, &granted.record, ke3) {
		return
	}
	ending, err := store.End(r.Context(), s.store, s.blobs, &granted.record, store.StateDeleted, time.Now())
	s.answerEnding(w, r, &granted.record, store.StateDeleted, ending, err)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	granted, ok := s.authorizedOpenable(w, r, seal.Manage)
	defer granted.forget()
	if !ok {
		return
	}
	ending, err := store.End(r.Context(), s.store, s.blobs, &granted.record, store.StateCancelled, time.Now())
	s.answerEnding(w, r, &granted.record, store.StateCancelled, ending, err)
}

// answerEnding answers 204 to the request that recorded the first ending, even when a concurrent release finishes it,
// and counts the ending only for that request.
func (s *Server) answerEnding(
	w http.ResponseWriter,
	r *http.Request,
	record *store.Record,
	requested store.State,
	ending store.Ending,
	err error,
) {
	s.leaseTimers.Cancel(record.Row.Locator)
	if ending.Recorded {
		s.count(statistics.MetricEnded, 1)
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeCurrentGone(w, r, record)
	case err != nil:
		s.writeInternal(w, r, err)
	case ending.State != requested:
		writeGone(w, string(ending.State))
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	granted, ok := s.authorized(w, r, seal.Manage)
	defer granted.forget()
	if !ok {
		return
	}
	now := time.Now()
	record := &granted.record
	s.endLostMessage(r.Context(), record, now)
	message := record.Message
	if !record.HasBlob() {
		writeJSON(w, http.StatusOK, api.Status{
			State:         string(record.State(now)),
			FirstOpenedAt: formatOptionalTime(message.FirstOpenedAt),
			EndedAt:       formatEnd(record.EndedAt(now)),
		})
		return
	}
	writeJSON(w, http.StatusOK, api.Status{
		State:                    string(record.State(now)),
		OpensUsed:                int(message.OpensMax) - int(message.OpensRemaining),
		OpensMax:                 int(message.OpensMax),
		CreatedAt:                formatTime(message.CreatedAt),
		ExpiresAt:                formatTime(message.ExpiresAt),
		FirstOpenedAt:            formatOptionalTime(message.FirstOpenedAt),
		FailedPassphraseAttempts: message.FailCountTotal,
	})
}
