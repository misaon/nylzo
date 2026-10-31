package logging

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
)

var idLike = regexp.MustCompile(`[0-9a-fA-F]{32,}|[A-Za-z0-9_-]{22,}`)

func New(output io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{ReplaceAttr: minuteTime}))
}

func minuteTime(groups []string, attribute slog.Attr) slog.Attr {
	if attribute.Key == slog.TimeKey && len(groups) == 0 && attribute.Value.Kind() == slog.KindTime {
		attribute.Value = slog.TimeValue(attribute.Value.Time().UTC().Truncate(time.Minute))
	}
	return attribute
}

func ErrorText(err error) string {
	text := err.Error()
	walk(err, func(link error) bool {
		text = withoutPath(text, link)
		return true
	})
	return idLike.ReplaceAllString(text, "[redacted]")
}

// JoinedErrors flattens the errors that errors.Join or fmt.Errorf with several %w combine; other wrappers stay whole.
func JoinedErrors(err error) []error {
	var parts []error
	walk(err, func(link error) bool {
		if _, joined := link.(interface{ Unwrap() []error }); joined {
			return true
		}
		parts = append(parts, link)
		return false
	})
	return parts
}

func withoutPath(text string, err error) string {
	if pathErr, isPath := errors.AsType[*fs.PathError](err); isPath {
		return strings.ReplaceAll(text, pathErr.Error(), pathErr.Op+": "+pathErr.Err.Error())
	}
	if linkErr, isLink := errors.AsType[*os.LinkError](err); isLink {
		return strings.ReplaceAll(text, linkErr.Error(), linkErr.Op+": "+linkErr.Err.Error())
	}
	return text
}

func walk(err error, visit func(error) bool) {
	if err == nil || !visit(err) {
		return
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, inner := range joined.Unwrap() {
			walk(inner, visit)
		}
		return
	}
	walk(errors.Unwrap(err), visit)
}
