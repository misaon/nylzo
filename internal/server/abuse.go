package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/misaon/nylzo/internal/server/abuse"

	"github.com/misaon/nylzo/internal/server/logging"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
)

type abuseBody struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func (s *Server) handleAbuse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBody))
	var body abuseBody
	if err != nil || json.Unmarshal(raw, &body) != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if _, err := core.ParseID(body.ID); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
		return
	}
	if !s.allowHourly(w, r, "abuse", s.cfg.Rates.AbusePerHour) {
		return
	}
	now := s.clock()
	a, ok, err := s.authorize(ctx, r, body.ID, pepper.Read, raw)
	id := a.id
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if ok && a.rec.Message != nil && !ended(a.rec.Message, now) {
		err := s.reports.Record(ctx, now, id.String(), body.Reason)
		if errors.Is(err, abuse.ErrFull) {
			s.log.Error("abuse-file-full")
			setRetryAfter(w, abuseFullRetry)
			writeError(w, http.StatusServiceUnavailable, errUnavailable)
			return
		}
		if err != nil {
			s.log.Error("abuse-record", "err", logging.ErrorText(err))
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}
