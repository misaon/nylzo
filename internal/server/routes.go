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
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/crossorigin"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/transport"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	prefetchRetry = time.Minute
	throttleRetry = time.Second
	messageRoutes = "message"
)

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	onMessage := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, logging.InGroup(messageRoutes, handler))
	}
	message := api.MessagePath("{id}")
	mux.HandleFunc("GET "+api.InstancePath, s.handleInstance)
	mux.HandleFunc("HEAD "+api.InstancePath, handleAPINotFound)
	mux.HandleFunc("GET "+api.ChallengePath, s.handleChallenge)
	mux.HandleFunc("HEAD "+api.ChallengePath, handleAPINotFound)
	mux.HandleFunc("POST "+api.MessagesPath, s.handleCreate)
	onMessage("GET "+message, s.handleMetadata)
	onMessage("HEAD "+message, handleAPINotFound)
	onMessage("POST "+message+api.ClaimAction, s.handleClaim)
	onMessage("GET "+message+api.BlobAction, s.handleBlob)
	onMessage("HEAD "+message+api.BlobAction, handleAPINotFound)
	onMessage("POST "+message+api.AckAction, s.handleAck)
	onMessage("POST "+message+api.BurnAction, s.handleBurn)
	onMessage("POST "+message+api.CancelAction, s.handleCancel)
	onMessage("GET "+message+api.StatusAction, s.handleStatus)
	onMessage("HEAD "+message+api.StatusAction, handleAPINotFound)
	onMessage("POST "+api.AbusePath, s.handleAbuse)
	mux.HandleFunc(api.Prefix, handleAPINotFound)
	mux.HandleFunc("GET "+api.TransparencyPath, s.handleTransparency)
	mux.HandleFunc("GET "+api.HealthPath, handleHealth)
	mux.Handle("/", s.frontend)
	routed := logging.Route(mux)
	return s.requestCounts.Requests(inSecretMode(transport.CloseUnreadBodies(
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
		if !isAPI(r) {
			next.ServeHTTP(w, r)
			return
		}
		secretmode.Do(func() { next.ServeHTTP(w, r) })
	})
}

func isAPI(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, api.Prefix)
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

// noticeForwarded counts the requests whose forwarding header did not set the client address, by header name.
func (s *Server) noticeForwarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := ratelimit.ForwardingHeader(r)
		if header != "" && s.clientAddress(r) == ratelimit.PeerAddress(r.RemoteAddr) {
			s.events.WarnContext(r.Context(), "forwarded-header-ignored", "category", header)
		}
		next.ServeHTTP(w, r)
	})
}

type spentKey struct{}

// spendChallenges spends a signed request's challenge before any other check, whatever the answer will be.
func (s *Server) spendChallenges(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature, err := signing.ParseAuthorization(r.Header.Get("Authorization"))
		if !isAPI(r) || err != nil {
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
		guarded := r.URL.Path == core.LinkPath || strings.HasPrefix(r.URL.Path, "/m/") || isAPI(r)
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
		if !isAPI(r) {
			next.ServeHTTP(w, r)
			return
		}
		buckets := s.newClientBuckets(r)
		if !s.allowAPI(buckets) {
			writeRateLimited(w, throttleRetry)
			return
		}
		next.ServeHTTP(w, withClientBuckets(r, buckets))
	})
}

const timeFormat = time.RFC3339

var (
	errBadRequest     = api.ErrorResponse{Error: api.ErrorBadRequest}
	errClientOutdated = api.ErrorResponse{Error: api.ErrorClientOutdated}
	errContact        = api.ErrorResponse{Error: api.ErrorContact}
	errNotFound       = api.ErrorResponse{Error: api.ErrorNotFound}
	errConflict       = api.ErrorResponse{Error: api.ErrorConflict}
	errShareLost      = api.ErrorResponse{Error: api.ErrorShareLost}
	errLengthRequired = api.ErrorResponse{Error: api.ErrorLengthRequired}
	errTooLarge       = api.ErrorResponse{Error: api.ErrorTooLarge}
	errUnavailable    = api.ErrorResponse{Error: api.ErrorUnavailable}
	errWorkRequired   = api.ErrorResponse{Error: api.ErrorWork}
	errIDToken        = api.ErrorResponse{Error: api.ErrorIDToken}
	errCrossOrigin    = api.ErrorResponse{Error: api.ErrorCrossOrigin}
	errInternal       = api.ErrorResponse{Error: api.ErrorInternal}
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

func writeError(w http.ResponseWriter, status int, body api.ErrorResponse) {
	writeJSON(w, status, body)
}

func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, errNotFound)
}

func writeGone(w http.ResponseWriter, state string) {
	writeJSON(w, http.StatusGone, api.GoneResponse{Error: api.ErrorGone, State: state})
}

func writeRateLimited(w http.ResponseWriter, wait time.Duration) {
	setRetryAfter(w, wait)
	writeJSON(
		w,
		http.StatusTooManyRequests,
		api.RateLimitedResponse{Error: api.ErrorRateLimited, RetryAfter: retrySeconds(wait)},
	)
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
	s.events.Log(r.Context(), entry.level, "handler", "err", entry.text)
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
