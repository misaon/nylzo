package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"

	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/webclient"
)

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webclient.SetSecurityHeaders(w.Header(), isHomePage(r))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) noticeForwarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := ratelimit.ForwardingHeader(r)
		if header != "" && s.clientAddr(r) == ratelimit.PeerAddress(r.RemoteAddr) {
			hour := s.clock().UTC().Truncate(time.Hour).Unix()
			for last := s.forwardNotice.Load(); hour > last || hour < last-int64(time.Hour/time.Second); last = s.forwardNotice.Load() {
				if s.forwardNotice.CompareAndSwap(last, hour) {
					s.log.Warn("forwarded-header-ignored", "header", header)
					break
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isHomePage(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && webclient.IsHomePath(r.URL.Path)
}

func (s *Server) prefetchGuard(next http.Handler) http.Handler {
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

func (s *Server) allowAPI(r *http.Request) bool {
	now := s.clock()
	addr := s.clientAddr(r)
	return s.sources.Allow(s.addressKey.Source(addr), now) &&
		s.prefixes.Allow(s.addressKey.Range(addr), now) &&
		s.instance.Allow(ratelimit.Bucket{}, now)
}

type spentKey struct{}

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
		inner := r
		if spent {
			inner = r.WithContext(context.WithValue(r.Context(), spentKey{}, &signature.Challenge))
		}
		next.ServeHTTP(w, inner)
	})
}

func spentBy(r *http.Request, challenge [core.ChallengeLen]byte) bool {
	spent, ok := r.Context().Value(spentKey{}).(*[core.ChallengeLen]byte)
	return ok && *spent == challenge
}
