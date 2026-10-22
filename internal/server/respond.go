package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

const (
	pendingRetry   = time.Minute
	powBusyRetry   = 5 * time.Second
	abuseFullRetry = time.Hour
	prefetchRetry  = time.Minute
	throttleRetry  = time.Second
)

func retrySeconds(d time.Duration) int64 {
	return max(1, ceilSeconds(d))
}

func setRetryAfter(w http.ResponseWriter, d time.Duration) {
	w.Header().Set("Retry-After", strconv.FormatInt(retrySeconds(d), 10))
}

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
	errPoWRequired    = apiError{Error: "pow"}
	errInternal       = apiError{Error: "internal"}
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		body = []byte(`{"error":"internal"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func writeError(w http.ResponseWriter, status int, e apiError) {
	writeJSON(w, status, e)
}

func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, errNotFound)
}

func writeGone(w http.ResponseWriter, state string) {
	writeJSON(w, http.StatusGone, gone{Error: "gone", State: state})
}

func writeRateLimited(w http.ResponseWriter, d time.Duration) {
	setRetryAfter(w, d)
	writeJSON(w, http.StatusTooManyRequests, rateLimited{Error: "rate_limited", RetryAfter: retrySeconds(d)})
}
