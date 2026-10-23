package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

const apiCSP = "default-src 'none'; frame-ancestors 'none'"

const cspTemplate = "default-src 'none'; script-src 'nonce-%s'; style-src 'nonce-%s'; connect-src 'self'; img-src 'self'; font-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; require-trusted-types-for 'script'; trusted-types svelte-trusted-html nylzo-kdf"

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
		h.Set("Content-Security-Policy", apiCSP)
		next.ServeHTTP(w, r)
	})
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
	return s.sources.allow(s.addrKey.bucket(addr, false), now) &&
		s.prefixes.allow(s.addrKey.bucket(addr, true), now) &&
		s.bucket.allow(singleKey, now)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return io.Copy(w.ResponseWriter, r)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

var objectRoutes = map[string]bool{
	"POST /api/v1/messages":            true,
	"POST /api/v1/messages/{id}/claim": true,
	"GET /api/v1/messages/{id}/blob":   true,
}

type routeKey struct{}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		route := new(string)
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), routeKey{}, route)))
		attrs := []any{"method", r.Method, "route", *route, "status", sw.status}
		if *route != "" && !objectRoutes[*route] {
			attrs = append(attrs, "duration_ms", float64(time.Since(start).Microseconds())/1000)
		}
		s.log.Info("request", attrs...)
	})
}

func recordRoute(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if route, ok := r.Context().Value(routeKey{}).(*string); ok {
			*route = r.Pattern
		}
	})
}
