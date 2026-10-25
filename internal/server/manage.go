package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
)

const (
	maxAbuseBody   = 4096
	abuseFullRetry = time.Hour
)

type statusResponse struct {
	State                    string `json:"state"`
	OpensUsed                int    `json:"opens_used"`
	OpensMax                 int    `json:"opens_max"`
	CreatedAt                string `json:"created_at,omitempty"`
	ExpiresAt                string `json:"expires_at,omitempty"`
	FirstOpenedAt            string `json:"first_opened_at,omitempty"`
	EndedAt                  string `json:"ended_at,omitempty"`
	FailedPassphraseAttempts int    `json:"failed_passphrase_attempts"`
}

type abuseRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func (s *Server) handleBurn(w http.ResponseWriter, r *http.Request) {
	s.end(w, r, pepper.Read, store.StateDeleted)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	s.end(w, r, pepper.Manage, store.StateCancelled)
}

// end answers 204 to the request that recorded the first ending, even when a concurrent release finishes it.
func (s *Server) end(w http.ResponseWriter, r *http.Request, purpose pepper.Purpose, state store.State) {
	granted, ok := s.authorizedLive(w, r, purpose)
	if !ok {
		return
	}
	id := granted.id.String()
	final, err := store.EndMessage(r.Context(), s.store, s.blobs, id, state, s.clock())
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeCurrentGone(w, r, id)
	case err != nil:
		s.writeInternal(w, r, err)
	case final != state:
		writeGone(w, string(final))
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	granted, ok := s.authorized(w, r, pepper.Manage)
	if !ok {
		return
	}
	if tombstone := granted.record.Tombstone; tombstone != nil {
		writeJSON(w, http.StatusOK, statusResponse{
			State:         string(tombstone.State),
			FirstOpenedAt: formatOptionalTime(tombstone.FirstOpenedAt),
			EndedAt:       formatTime(tombstone.EndedAt),
		})
		return
	}
	message := granted.record.Message
	writeJSON(w, http.StatusOK, statusResponse{
		State:                    string(liveState(message, s.clock())),
		OpensUsed:                int(message.OpensMax) - int(message.OpensRemaining),
		OpensMax:                 int(message.OpensMax),
		CreatedAt:                formatTime(message.CreatedAt),
		ExpiresAt:                formatTime(message.ExpiresAt),
		FirstOpenedAt:            formatOptionalTime(message.FirstOpenedAt),
		FailedPassphraseAttempts: message.FailCountTotal,
	})
}

// handleAbuse answers 202 for known, unknown and malformed ids alike and stores reports only for live messages.
func (s *Server) handleAbuse(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAbuseBody))
	var report abuseRequest
	if err != nil || json.Unmarshal(raw, &report) != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if _, idErr := core.ParseID(report.ID); idErr != nil {
		writeAccepted(w)
		return
	}
	if !s.allowHourly(w, r, "abuse", s.settings.Rates.AbusePerHour) {
		return
	}
	now := s.clock()
	granted, ok, err := s.authorize(r.Context(), r, report.ID, pepper.Read, raw)
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	live := ok && granted.record.Message != nil && !ended(granted.record.Message, now)
	if live && !s.recordAbuse(w, r, now, granted.id.String(), report.Reason) {
		return
	}
	writeAccepted(w)
}

func (s *Server) recordAbuse(w http.ResponseWriter, r *http.Request, now time.Time, id, reason string) bool {
	err := s.reports.Record(r.Context(), now, id, reason)
	switch {
	case errors.Is(err, abuse.ErrFull):
		s.logger.ErrorContext(r.Context(), "abuse-file-full")
		setRetryAfter(w, abuseFullRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return false
	case err != nil:
		s.logger.ErrorContext(r.Context(), "abuse-record", "err", logging.ErrorText(err))
	default:
	}
	return true
}

func writeAccepted(w http.ResponseWriter) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}
