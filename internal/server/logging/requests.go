package logging

import (
	"cmp"
	"context"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sync"
)

const otherMethod = "OTHER"

var routedMethods = map[string]bool{http.MethodGet: true, http.MethodHead: true, http.MethodPost: true}

type routeKey struct{}

type requestKind struct {
	method string
	route  string
	status int
}

// RequestCounts logs no line per request: it counts requests by method, route and status and writes the counts
// once a minute, sorted, so neither the time nor the order of a line tells when one request ended (spec §10).
type RequestCounts struct {
	logger *slog.Logger
	mutex  sync.Mutex
	counts map[requestKind]int
}

func NewRequestCounts(logger *slog.Logger) *RequestCounts {
	return &RequestCounts{logger: logger, counts: map[requestKind]int{}}
}

// Requests counts every request; the mux must be wrapped in Route so the count can name its pattern.
func (c *RequestCounts) Requests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w}
		route := new(string)
		next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), routeKey{}, route)))
		method := r.Method
		if !routedMethods[method] {
			method = otherMethod
		}
		c.mutex.Lock()
		c.counts[requestKind{method: method, route: *route, status: recorder.status}]++
		c.mutex.Unlock()
	})
}

func (c *RequestCounts) Flush() {
	c.mutex.Lock()
	counts := c.counts
	c.counts = map[requestKind]int{}
	c.mutex.Unlock()
	kinds := slices.SortedFunc(maps.Keys(counts), func(first, second requestKind) int {
		return cmp.Or(
			cmp.Compare(first.route, second.route),
			cmp.Compare(first.method, second.method),
			cmp.Compare(first.status, second.status),
		)
	})
	for _, kind := range kinds {
		c.logger.Info(
			"requests",
			"method", kind.method,
			"route", kind.route,
			"status", kind.status,
			"count", counts[kind],
		)
	}
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

func (w *statusRecorder) ReadFrom(source io.Reader) (int64, error) {
	w.markOK()
	return io.Copy(w.ResponseWriter, source)
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
