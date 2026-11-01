package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/crossorigin"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	prefetchRetry = time.Minute
	throttleRetry = time.Second
)

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/instance", s.handleInstance)
	mux.HandleFunc("HEAD /api/v1/instance", handleAPINotFound)
	mux.HandleFunc("GET /api/v1/challenge", s.handleChallenge)
	mux.HandleFunc("HEAD /api/v1/challenge", handleAPINotFound)
	mux.HandleFunc("POST /api/v1/messages", s.handleCreate)
	mux.HandleFunc("GET /api/v1/messages/{id}", s.handleMetadata)
	mux.HandleFunc("HEAD /api/v1/messages/{id}", handleAPINotFound)
	mux.HandleFunc("POST /api/v1/messages/{id}/claim", s.handleClaim)
	mux.HandleFunc("GET /api/v1/messages/{id}/blob", s.handleBlob)
	mux.HandleFunc("HEAD /api/v1/messages/{id}/blob", handleAPINotFound)
	mux.HandleFunc("POST /api/v1/messages/{id}/ack", s.handleAck)
	mux.HandleFunc("POST /api/v1/messages/{id}/burn", s.handleBurn)
	mux.HandleFunc("POST /api/v1/messages/{id}/cancel", s.handleCancel)
	mux.HandleFunc("GET /api/v1/messages/{id}/status", s.handleStatus)
	mux.HandleFunc("HEAD /api/v1/messages/{id}/status", handleAPINotFound)
	mux.HandleFunc("POST /api/v1/abuse", s.handleAbuse)
	mux.HandleFunc("/api/", handleAPINotFound)
	mux.HandleFunc("GET "+messagePagePath, s.handleMessagePage)
	mux.HandleFunc("GET "+core.LinkPath, s.handleReadPage)
	mux.HandleFunc("GET /assets/{name}", s.handleAsset)
	mux.HandleFunc("GET /{$}", s.pageHandler("/"))
	mux.HandleFunc("GET /cs/{$}", s.pageHandler("/cs/"))
	mux.HandleFunc("GET /cs", handleCzechHome)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
	for _, path := range []string{"/sent", "/report", "/transparency"} {
		mux.HandleFunc("GET "+path, s.pageHandler(path))
	}
	mux.HandleFunc("GET "+webclient.ManifestPath, s.handleClientManifest)
	mux.HandleFunc("GET "+TransparencyPath, s.handleTransparency)
	mux.HandleFunc("GET /healthz", handleHealth)
	routed := logging.Route(mux)
	return s.requestCounts.Requests(inSecretMode(closeUnreadBodies(
		s.noticeForwarded(securityHeaders(crossorigin.Guard(
			s.settings.PublicURL,
			http.HandlerFunc(refuseCrossOrigin),
			s.spendChallenges(prefetchGuard(s.throttle(routed))),
		))),
	)),
	)
}

// inSecretMode runs every API request, which may carry public keys and open record keys, blob keys and erase salts,
// in secret mode: its registers and stack are erased when it returns, its heap allocations once unreachable (A4-2).
func inSecretMode(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		secretmode.Do(func() { next.ServeHTTP(w, r) })
	})
}

func refuseCrossOrigin(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusForbidden, errCrossOrigin)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexable := (r.Method == http.MethodGet || r.Method == http.MethodHead) && webclient.IsHomePath(r.URL.Path)
		webclient.SetSecurityHeaders(w.Header(), indexable)
		next.ServeHTTP(w, r)
	})
}

// noticeForwarded warns at most once per hour that a forwarding header did not set the client address.
func (s *Server) noticeForwarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := ratelimit.ForwardingHeader(r)
		if header != "" && s.clientAddress(r) == ratelimit.PeerAddress(r.RemoteAddr) &&
			s.firstNoticeThisHour(time.Now()) {
			s.logger.WarnContext(r.Context(), "forwarded-header-ignored", "header", header)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) firstNoticeThisHour(now time.Time) bool {
	hour := now.UTC().Truncate(time.Hour).Unix()
	hourSeconds := int64(time.Hour / time.Second)
	for last := s.forwardedHour.Load(); hour > last || hour < last-hourSeconds; last = s.forwardedHour.Load() {
		if s.forwardedHour.CompareAndSwap(last, hour) {
			return true
		}
	}
	return false
}

type spentKey struct{}

// spendChallenges spends a signed request's challenge before any other check, whatever the answer will be.
func (s *Server) spendChallenges(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature, err := signing.ParseAuthorization(r.Header.Get("Authorization"))
		if !strings.HasPrefix(r.URL.Path, "/api/") || err != nil {
			next.ServeHTTP(w, r)
			return
		}
		spent, err := s.challenges.Spend(r.Context(), signature.Challenge, time.Now())
		if err != nil {
			s.writeInternal(w, r, err)
			return
		}
		if spent {
			r = r.WithContext(context.WithValue(r.Context(), spentKey{}, &signature.Challenge))
		}
		next.ServeHTTP(w, r)
	})
}

func spentBy(r *http.Request, challenge [signing.ChallengeLen]byte) bool {
	spent, ok := r.Context().Value(spentKey{}).(*[signing.ChallengeLen]byte)
	return ok && *spent == challenge
}

func prefetchGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded := r.URL.Path == core.LinkPath || strings.HasPrefix(r.URL.Path, "/m/") ||
			strings.HasPrefix(r.URL.Path, "/api/")
		if guarded && r.Header.Get("Sec-Purpose") != "" {
			setRetryAfter(w, prefetchRetry)
			writeError(w, http.StatusServiceUnavailable, errUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) throttle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !s.allowAPI(r) {
			writeRateLimited(w, throttleRetry)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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
	errClientOutdated = apiError{Error: "client_outdated"}
	errContact        = apiError{Error: "contact"}
	errNotFound       = apiError{Error: "not_found"}
	errConflict       = apiError{Error: "conflict"}
	errShareLost      = apiError{Error: "share_lost"}
	errLengthRequired = apiError{Error: "length_required"}
	errTooLarge       = apiError{Error: "too_large"}
	errUnavailable    = apiError{Error: "unavailable"}
	errWorkRequired   = apiError{Error: "pow"}
	errIDToken        = apiError{Error: "id_token"}
	errCrossOrigin    = apiError{Error: "cross_origin"}
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

func formatTime(moment time.Time) string {
	return moment.UTC().Format(timeFormat)
}

func formatOptionalTime(moment *time.Time) string {
	if moment == nil {
		return ""
	}
	return formatTime(*moment)
}
