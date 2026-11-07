package logging

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"
)

const (
	categoryKey = "category"
	errorKey    = "err"
)

type eventKind struct {
	message  string
	category string
	level    slog.Level
}

type eventTally struct {
	count     int
	errorText string
}

// Events holds the lines about requests and messages instead of writing them: it counts them by level, message and
// category, keeps the first error text and drops every other attribute, so a line neither dates nor sizes an event.
type Events struct {
	handler slog.Handler
	mutex   sync.Mutex
	tallies map[eventKind]*eventTally
}

func NewEvents(logger *slog.Logger) *Events {
	return &Events{handler: logger.Handler(), tallies: map[eventKind]*eventTally{}}
}

// Logger returns a logger whose lines Events holds until Flush.
func (e *Events) Logger() *slog.Logger {
	return slog.New(&heldHandler{events: e})
}

// Flush writes one line per kind of event with its count, sorted, and starts counting again.
func (e *Events) Flush(ctx context.Context) {
	e.mutex.Lock()
	tallies := e.tallies
	e.tallies = map[eventKind]*eventTally{}
	e.mutex.Unlock()
	kinds := slices.SortedFunc(maps.Keys(tallies), func(first, second eventKind) int {
		return cmp.Or(
			cmp.Compare(first.message, second.message),
			cmp.Compare(first.category, second.category),
			cmp.Compare(first.level, second.level),
		)
	})
	for _, kind := range kinds {
		if !e.handler.Enabled(ctx, kind.level) {
			continue
		}
		record := slog.NewRecord(time.Now(), kind.level, kind.message, 0)
		if kind.category != "" {
			record.AddAttrs(slog.String(categoryKey, kind.category))
		}
		record.AddAttrs(slog.Int(countKey, tallies[kind].count))
		if errorText := tallies[kind].errorText; errorText != "" {
			record.AddAttrs(slog.String(errorKey, errorText))
		}
		_ = e.handler.Handle(ctx, record)
	}
}

func (e *Events) add(kind eventKind, errorText string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	tally, found := e.tallies[kind]
	if !found {
		tally = &eventTally{}
		e.tallies[kind] = tally
	}
	tally.count++
	if tally.errorText == "" {
		tally.errorText = errorText
	}
}

type heldHandler struct {
	events     *Events
	attributes []slog.Attr
	grouped    bool
}

func (h *heldHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.events.handler.Enabled(ctx, level)
}

//nolint:gocritic // hugeParam: slog.Handler takes the record by value.
func (h *heldHandler) Handle(_ context.Context, record slog.Record) error {
	kind := eventKind{message: record.Message, level: record.Level}
	var errorText string
	read := func(attribute slog.Attr) bool {
		switch attribute.Key {
		case categoryKey:
			kind.category = attribute.Value.String()
		case errorKey:
			if errorText == "" {
				errorText = heldErrorText(attribute.Value)
			}
		default:
		}
		return true
	}
	for _, attribute := range h.attributes {
		read(attribute)
	}
	if !h.grouped {
		record.Attrs(read)
	}
	h.events.add(kind, errorText)
	return nil
}

func (h *heldHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	if h.grouped {
		return h
	}
	return &heldHandler{events: h.events, attributes: append(slices.Clip(h.attributes), attributes...)}
}

func (h *heldHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &heldHandler{events: h.events, attributes: h.attributes, grouped: true}
}

func heldErrorText(value slog.Value) string {
	if err, isError := value.Any().(error); isError {
		return ErrorText(err)
	}
	return value.String()
}
