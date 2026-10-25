package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
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
	return logging.Requests(
		s.logger,
		s.noticeForwarded(securityHeaders(s.spendChallenges(prefetchGuard(s.throttle(routed))))),
	)
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
		if header != "" && s.clientAddress(r) == ratelimit.PeerAddress(r.RemoteAddr) && s.firstNoticeThisHour() {
			s.logger.WarnContext(r.Context(), "forwarded-header-ignored", "header", header)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) firstNoticeThisHour() bool {
	hour := s.clock().UTC().Truncate(time.Hour).Unix()
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
		signature, err := core.ParseAuthorization(r.Header.Get("Authorization"))
		if !strings.HasPrefix(r.URL.Path, "/api/") || err != nil {
			next.ServeHTTP(w, r)
			return
		}
		spent, err := s.challenges.Spend(r.Context(), signature.Challenge, s.clock())
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

func spentBy(r *http.Request, challenge [core.ChallengeLen]byte) bool {
	spent, ok := r.Context().Value(spentKey{}).(*[core.ChallengeLen]byte)
	return ok && *spent == challenge
}

func prefetchGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded := strings.HasPrefix(r.URL.Path, "/m/") || strings.HasPrefix(r.URL.Path, "/api/")
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
