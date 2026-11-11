package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/report"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
)

const (
	maxAbuseBody   = 4096
	abuseFullRetry = time.Hour
	abuseAccepted  = "accepted"
	abuseLimit     = "limit"
)

type abuseRequest struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	Contact   string `json:"contact"`
	GoodFaith bool   `json:"good_faith"`
}

// handleBurn deletes a message with a passphrase only with its verifier, checked and counted as for a claim (F05).
func (s *Server) handleBurn(w http.ResponseWriter, r *http.Request) {
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	verifier, verifierSent, ok := parseVerifier(w, r)
	if !ok {
		return
	}
	granted, ok := s.authorizedLive(w, r, seal.Read)
	defer granted.forget()
	if !ok {
		return
	}
	record := &granted.record
	if record.Message.PassphraseRequired() && !verifierSent {
		writeError(w, http.StatusBadRequest, errClientOutdated)
		return
	}
	var salt seal.EraseSalt
	defer clear(salt[:])
	if record.Message.PassphraseRequired() && !s.readEraseSalt(w, r, record, &salt) {
		return
	}
	proof := s.verifierProof(r, &verifier, verifierSent, granted.signature.Challenge, &salt)
	final, err := store.Burn(r.Context(), s.store, s.blobs, record, proof, time.Now())
	s.answerEnding(w, r, record, store.StateDeleted, final, err)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	granted, ok := s.authorizedLive(w, r, seal.Manage)
	defer granted.forget()
	if !ok {
		return
	}
	final, err := store.End(r.Context(), s.store, s.blobs, &granted.record, store.StateCancelled, time.Now())
	s.answerEnding(w, r, &granted.record, store.StateCancelled, final, err)
}

// answerEnding answers 204 to the request that recorded the first ending, even when a concurrent release finishes it.
func (s *Server) answerEnding(
	w http.ResponseWriter,
	r *http.Request,
	record *store.Record,
	requested, final store.State,
	err error,
) {
	if refusal, refused := errors.AsType[*store.PassphraseError](err); refused {
		writePassphraseRefusal(w, refusal)
		return
	}
	s.leaseTimers.Cancel(record.Row.Locator)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeCurrentGone(w, r, record)
	case err != nil:
		s.writeInternal(w, r, err)
	case final != requested:
		writeGone(w, string(final))
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	granted, ok := s.authorized(w, r, seal.Manage)
	defer granted.forget()
	if !ok {
		return
	}
	now := time.Now()
	record := &granted.record
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

// handleAbuse answers the same 202 for unknown, ended and malformed ids and stores reports only for live messages.
func (s *Server) handleAbuse(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAbuseBody))
	defer clear(raw)
	var submitted abuseRequest
	if err != nil || json.Unmarshal(raw, &submitted) != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	contact, err := report.ParseContact(submitted.Contact)
	if err != nil {
		writeError(w, http.StatusBadRequest, errContact)
		return
	}
	if _, idErr := core.ParseID(submitted.ID); idErr != nil {
		writeAbuseStatus(w, abuseAccepted)
		return
	}
	if !s.allowHourly(w, r, "abuse", s.settings.Rates.AbusePerHour) {
		return
	}
	now := time.Now()
	granted, ok, err := s.authorize(r.Context(), r, submitted.ID, seal.Read, raw)
	defer granted.forget()
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if !ok || granted.record.Ended(now) {
		waitForTheFloor(r, now)
		writeAbuseStatus(w, abuseAccepted)
		return
	}
	s.recordAbuse(w, r, now, &granted.record, &abuse.Report{
		ID:        granted.id.String(),
		Reason:    submitted.Reason,
		Contact:   contact,
		GoodFaith: submitted.GoodFaith,
	})
}

// recordAbuse counts the report in the record first and takes the count back when the report cannot be stored.
func (s *Server) recordAbuse(
	w http.ResponseWriter,
	r *http.Request,
	now time.Time,
	record *store.Record,
	submitted *abuse.Report,
) {
	counted, err := store.CountReport(r.Context(), s.store, record, now)
	if _, gone := errors.AsType[*store.GoneError](err); gone || errors.Is(err, store.ErrNotFound) {
		writeAbuseStatus(w, abuseAccepted)
		return
	}
	switch {
	case err != nil:
		s.writeInternal(w, r, err)
		return
	case !counted:
		writeAbuseStatus(w, abuseLimit)
		return
	}
	err = s.reports.Record(now, submitted)
	if err == nil {
		writeAbuseStatus(w, abuseAccepted)
		return
	}
	if uncountErr := store.UncountReport(r.Context(), s.store, record, now); uncountErr != nil {
		s.events.ErrorContext(r.Context(), "abuse-release", "err", logging.ErrorText(uncountErr))
	}
	if errors.Is(err, abuse.ErrFull) {
		s.events.ErrorContext(r.Context(), "abuse-file-full")
		setRetryAfter(w, abuseFullRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return
	}
	s.writeInternal(w, r, err)
}

func writeAbuseStatus(w http.ResponseWriter, status string) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": status})
}
