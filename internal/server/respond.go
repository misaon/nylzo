package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/server/logging"
)

const timeFormat = time.RFC3339

type apiError struct {
	Error string `json:"error"`
}

type rateLimited struct {
	Error      string `json:"error"`
	RetryAfter int64  `json:"retry_after"`
}

type passphraseRejected struct {
	Error        string `json:"error"`
	RetryAfter   int64  `json:"retry_after"`
	AttemptsLeft int    `json:"attempts_left"`
}

type gone struct {
	Error string `json:"error"`
	State string `json:"state"`
}

var (
	errBadRequest     = apiError{Error: "bad_request"}
	errNotFound       = apiError{Error: "not_found"}
	errConflict       = apiError{Error: "conflict"}
	errLengthRequired = apiError{Error: "length_required"}
	errTooLarge       = apiError{Error: "too_large"}
	errUnavailable    = apiError{Error: "unavailable"}
	errWorkRequired   = apiError{Error: "pow"}
	errIDToken        = apiError{Error: "id_token"}
	errInternal       = apiError{Error: "internal"}
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte(`{"error":"internal"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func writeError(w http.ResponseWriter, status int, apiErr apiError) {
	writeJSON(w, status, apiErr)
}

func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, errNotFound)
}

func writeGone(w http.ResponseWriter, state string) {
	writeJSON(w, http.StatusGone, gone{Error: "gone", State: state})
}

func writeRateLimited(w http.ResponseWriter, wait time.Duration) {
	setRetryAfter(w, wait)
	writeJSON(w, http.StatusTooManyRequests, rateLimited{Error: "rate_limited", RetryAfter: retrySeconds(wait)})
}

func setRetryAfter(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.FormatInt(retrySeconds(wait), 10))
}

func retrySeconds(wait time.Duration) int64 {
	return max(1, ceilSeconds(wait))
}

func ceilSeconds(wait time.Duration) int64 {
	if wait <= 0 {
		return 0
	}
	return int64((wait + time.Second - 1) / time.Second)
}

func (s *Server) writeInternal(w http.ResponseWriter, r *http.Request, err error) {
	entry := failure(r.Context(), err)
	s.logger.Log(r.Context(), entry.level, "handler", "route", r.Pattern, "err", entry.text)
	if entry.clientGone {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	writeError(w, http.StatusInternalServerError, errInternal)
}

type failureEntry struct {
	clientGone bool
	level      slog.Level
	text       string
}

// failure logs at info level a failure that only means that the client went away.
func failure(ctx context.Context, err error) failureEntry {
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return failureEntry{clientGone: true, level: slog.LevelInfo, text: "client went away"}
	}
	return failureEntry{level: slog.LevelError, text: logging.ErrorText(err)}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeFormat)
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}
