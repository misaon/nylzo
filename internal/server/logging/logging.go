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
	for _, link := range errorChain(err) {
		text = withoutPath(text, link)
	}
	return idLike.ReplaceAllString(text, "[redacted]")
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

func errorChain(err error) []error {
	if err == nil {
		return nil
	}
	chain := append([]error{err}, errorChain(errors.Unwrap(err))...)
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, inner := range joined.Unwrap() {
			chain = append(chain, errorChain(inner)...)
		}
	}
	return chain
}
