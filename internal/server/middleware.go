package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/server/ratelimit"
)

const apiCSP = "default-src 'none'; frame-ancestors 'none'"

const cspTemplate = "default-src 'none'; script-src 'nonce-%s'; style-src 'nonce-%s'; connect-src 'self'; img-src 'self'; font-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; require-trusted-types-for 'script'; trusted-types svelte-trusted-html nylzo-kdf"

const robotsTag = "noindex, nofollow"

var securityHeaderValues = map[string]string{
	"Referrer-Policy":              "no-referrer",
	"Cache-Control":                "no-store",
	"X-Content-Type-Options":       "nosniff",
	"Cross-Origin-Opener-Policy":   "same-origin",
	"Cross-Origin-Resource-Policy": "same-origin",
	"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	"Permissions-Policy":           "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		for name, value := range securityHeaderValues {
			h.Set(name, value)
		}
		if !isHomePage(r) {
			h.Set("X-Robots-Tag", robotsTag)
		}
		h.Set("Content-Security-Policy", apiCSP)
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
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && isHomePath(r.URL.Path)
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
