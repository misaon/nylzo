package logging

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// objectRoutes take as long as their object is large, so their latency would tie a log line to a message size.
var objectRoutes = map[string]bool{
	"POST /api/v1/messages":            true,
	"POST /api/v1/messages/{id}/claim": true,
	"GET /api/v1/messages/{id}/blob":   true,
}

type routeKey struct{}

// Requests logs one line per request; the mux must be wrapped in Route so the line can name its pattern.
func Requests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		route := new(string)
		next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), routeKey{}, route)))
		attributes := []any{"method", r.Method, "route", *route, "status", recorder.status}
		if *route != "" && !objectRoutes[*route] {
			attributes = append(attributes, "duration_ms", float64(time.Since(start).Microseconds())/1000)
		}
		logger.InfoContext(r.Context(), "request", attributes...)
	})
}

func Route(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if route, ok := r.Context().Value(routeKey{}).(*string); ok {
			*route = r.Pattern
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter

	status int
}

func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) ReadFrom(r io.Reader) (int64, error) {
	w.markOK()
	return io.Copy(w.ResponseWriter, r)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
	w.markOK()
	return w.ResponseWriter.Write(body)
}

func (w *statusRecorder) markOK() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
}
