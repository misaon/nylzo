package logging

import (
	"bytes"
	"context"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/server/ratelimit"
)

const (
	httpErrorRate  = 1
	httpErrorBurst = 20
)

var httpErrorCategories = []struct {
	prefix string
	name   string
	level  slog.Level
}{
	{"http: Accept error", "accept", slog.LevelError},
	{"http: panic serving", "panic", slog.LevelError},
	{"http2: panic serving", "panic", slog.LevelError},
	{"http: TLS handshake error", "tls-handshake", slog.LevelWarn},
	{"http2: ", "http2", slog.LevelWarn},
	{"http: superfluous response.WriteHeader", "superfluous-write-header", slog.LevelWarn},
}

type levelLimit struct {
	limiter *ratelimit.Limiter
	dropped int
}

type httpErrorLog struct {
	logger   *slog.Logger
	clock    func() time.Time
	mutex    sync.Mutex
	warnings levelLimit
	errors   levelLimit
}

// NewHTTPErrorLog logs net/http errors by category only, because their text can carry a peer address.
func NewHTTPErrorLog(logger *slog.Logger, clock func() time.Time) *log.Logger {
	sink := &httpErrorLog{
		logger:   logger,
		clock:    clock,
		warnings: levelLimit{limiter: ratelimit.NewLimiter(httpErrorRate, httpErrorBurst)},
		errors:   levelLimit{limiter: ratelimit.NewLimiter(httpErrorRate, httpErrorBurst)},
	}
	return log.New(sink, "", 0)
}

func (e *httpErrorLog) Write(line []byte) (int, error) {
	category, level := "other", slog.LevelWarn
	for _, known := range httpErrorCategories {
		if bytes.HasPrefix(line, []byte(known.prefix)) {
			category, level = known.name, known.level
			break
		}
	}
	limit := &e.warnings
	if level >= slog.LevelError {
		limit = &e.errors
	}
	dropped, allowed := e.take(limit)
	if allowed {
		e.logger.Log(context.Background(), level, "net/http error", "category", category, "dropped", dropped)
	}
	return len(line), nil
}

func (e *httpErrorLog) take(limit *levelLimit) (int, bool) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if !limit.limiter.Allow(ratelimit.Bucket{}, e.clock()) {
		limit.dropped++
		return 0, false
	}
	dropped := limit.dropped
	limit.dropped = 0
	return dropped, true
}
