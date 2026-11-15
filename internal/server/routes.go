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
	"github.com/misaon/nylzo/internal/server/proof"
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
	onMessageID := func(pattern string, handler http.HandlerFunc) {
		onMessage(pattern, func(w http.ResponseWriter, r *http.Request) {
			if validPathID(w, r) && s.allowRead(w, r) {
				handler(w, r)
			}
		})
	}
	message := api.MessagePath("{id}")
	mux.HandleFunc("GET "+api.InstancePath, s.handleInstance)
	mux.HandleFunc("HEAD "+api.InstancePath, handleAPINotFound)
	mux.HandleFunc("GET "+api.ChallengePath, s.handleChallenge)
	mux.HandleFunc("HEAD "+api.ChallengePath, handleAPINotFound)
	mux.HandleFunc("POST "+api.MessagesPath, s.handleCreate)
	onMessageID("GET "+message, s.handleMetadata)
	onMessage("HEAD "+message, handleAPINotFound)
	onMessageID("POST "+message+api.HandshakeAction, s.handleHandshake)
	onMessageID("POST "+message+api.ClaimAction, s.handleClaim)
	onMessageID("GET "+message+api.BlobAction, s.handleBlob)
	onMessage("HEAD "+message+api.BlobAction, handleAPINotFound)
	onMessageID("POST "+message+api.AckAction, s.handleAck)
	onMessageID("POST "+message+api.BurnAction, s.handleBurn)
	onMessageID("POST "+message+api.CancelAction, s.handleCancel)
	onMessageID("GET "+message+api.StatusAction, s.handleStatus)
	onMessage("HEAD "+message+api.StatusAction, handleAPINotFound)
	mux.HandleFunc(api.Prefix, handleAPINotFound)
	mux.HandleFunc("GET "+api.TransparencyPath, s.handleTransparency)
	mux.HandleFunc("GET "+api.HealthPath, handleHealth)
	mux.Handle("/", s.frontend)
	routed := logging.Route(mux)
	return s.requestCounts.Requests(inSecretMode(transport.CloseUnreadBodies(
		s.noticeForwarded(securityHeaders(crossorigin.Guard(
			s.settings.PublicURL,
			http.HandlerFunc(refuseCrossOrigin),
			s.seeChallenges(prefetchGuard(s.throttle(routed))),
		))),
	)),
	)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeNotFound(w)
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
	writeError(w, http.StatusForbidden, api.ErrorCrossOrigin)
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
		if header != "" && s.proxies.ClientAddress(r) == ratelimit.PeerAddress(r.RemoteAddr) {
			s.events.WarnContext(r.Context(), "forwarded-header-ignored", "category", header)
		}
		next.ServeHTTP(w, r)
	})
}

type spentKey struct{}

type spentChallenge struct {
	challenge [signing.ChallengeLen]byte
	login     *proof.Login
	checked   bool
}

// seeChallenges uses up a signed request's challenge after the cross-origin gate and before any other check, so no
// answer leaves it usable; the login state of a handshake goes with the request, a refusal before its KE3 is checked
// takes the attempt back, and the state is wiped when the request ends.
func (s *Server) seeChallenges(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature, err := signing.ParseAuthorization(r.Header.Get("Authorization"))
		if isAPI(r) && err == nil {
			if accepted, login := s.challenges.Take(signature.Challenge, time.Now()); accepted {
				spent := &spentChallenge{challenge: signature.Challenge, login: login}
				r = r.WithContext(context.WithValue(r.Context(), spentKey{}, spent))
				if login != nil {
					defer login.Wipe()
					defer s.takeBackUnchecked(r)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func spentBy(r *http.Request, challenge [signing.ChallengeLen]byte) bool {
	spent, ok := r.Context().Value(spentKey{}).(*spentChallenge)
	return ok && spent.challenge == challenge
}

// takeLogin hands the login state of the handshake that issued the request's challenge, if one did, to the one check
// of the request's KE3.
func takeLogin(r *http.Request) *proof.Login {
	spent, ok := r.Context().Value(spentKey{}).(*spentChallenge)
	if !ok || spent.checked {
		return nil
	}
	spent.checked = true
	return spent.login
}

func prefetchGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded := r.URL.Path == core.LinkPath || strings.HasPrefix(r.URL.Path, "/m/") || isAPI(r)
		if guarded && r.Header.Get("Sec-Purpose") != "" {
			writeUnavailable(w, prefetchRetry)
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

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, api.ErrorResponse{Error: code})
}

func writeNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, api.ErrorNotFound)
}

func writeUnavailable(w http.ResponseWriter, wait time.Duration) {
	setRetryAfter(w, wait)
	writeError(w, http.StatusServiceUnavailable, api.ErrorUnavailable)
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
		writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
		return
	}
	writeError(w, http.StatusInternalServerError, api.ErrorInternal)
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
