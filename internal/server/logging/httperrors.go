package logging

import (
	"bytes"
	"log"
	"log/slog"
)

const httpErrorMessage = "net/http error"

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

type httpErrorLog struct {
	events *Events
}

// NewHTTPErrorLog holds net/http errors as events of their category only, because their text can carry a peer
// address and their time can date a request.
func NewHTTPErrorLog(events *Events) *log.Logger {
	return log.New(httpErrorLog{events: events}, "", 0)
}

func (e httpErrorLog) Write(line []byte) (int, error) {
	kind := eventKind{message: httpErrorMessage, category: "other", level: slog.LevelWarn}
	for _, known := range httpErrorCategories {
		if bytes.HasPrefix(line, []byte(known.prefix)) {
			kind.category, kind.level = known.name, known.level
			break
		}
	}
	e.events.add(kind, "")
	return len(line), nil
}
