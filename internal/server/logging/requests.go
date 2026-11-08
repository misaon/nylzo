package logging

import (
	"cmp"
	"context"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"sync"
)

const (
	otherMethod      = "OTHER"
	statusesPerClass = 100
	countKey         = "count"
)

var routedMethods = map[string]bool{http.MethodGet: true, http.MethodHead: true, http.MethodPost: true}

type routeKey struct{}

type route struct {
	pattern string
	group   string
}

type requestKind struct {
	method  string
	route   string
	status  int
	grouped bool
}

// RequestCounts logs no line per request: it counts requests by method, route and status, or by route group and
// status class, and writes the counts sorted when flushed, so neither the time nor the order of a line tells when
// one request ended (spec §10).
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
		routed := &route{}
		next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), routeKey{}, routed)))
		kind := requestKindOf(r.Method, routed, cmp.Or(recorder.status, http.StatusOK))
		c.mutex.Lock()
		c.counts[kind]++
		c.mutex.Unlock()
	})
}

func requestKindOf(method string, routed *route, status int) requestKind {
	if routed.group != "" {
		return requestKind{route: routed.group, status: status / statusesPerClass, grouped: true}
	}
	if !routedMethods[method] {
		method = otherMethod
	}
	return requestKind{method: method, route: routed.pattern, status: status}
}

func (c *RequestCounts) Flush(ctx context.Context) {
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
		if kind.grouped {
			c.logger.InfoContext(
				ctx,
				"requests",
				"route", kind.route,
				"status_class", strconv.Itoa(kind.status)+"xx",
				countKey, counts[kind],
			)
			continue
		}
		c.logger.InfoContext(
			ctx,
			"requests",
			"method", kind.method,
			"route", kind.route,
			"status", kind.status,
			countKey, counts[kind],
		)
	}
}

func Route(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if routed, ok := r.Context().Value(routeKey{}).(*route); ok {
			routed.pattern = r.Pattern
		}
	})
}

// InGroup counts the requests of a route under the name of its group and by status class only, so the counts of
// routes that act on a message do not tell which action a request took.
func InGroup(group string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if routed, ok := r.Context().Value(routeKey{}).(*route); ok {
			routed.group = group
		}
		next.ServeHTTP(w, r)
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
