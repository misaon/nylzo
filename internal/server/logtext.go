package server

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

var idLike = regexp.MustCompile(`[0-9a-fA-F]{32,}|[A-Za-z0-9_-]{22,}`)

func ErrorText(err error) string {
	text := err.Error()
	for _, e := range errorChain(err) {
		var pathErr *fs.PathError
		var linkErr *os.LinkError
		switch {
		case errors.As(e, &pathErr):
			text = strings.ReplaceAll(text, pathErr.Error(), pathErr.Op+": "+pathErr.Err.Error())
		case errors.As(e, &linkErr):
			text = strings.ReplaceAll(text, linkErr.Error(), linkErr.Op+": "+linkErr.Err.Error())
		}
	}
	return idLike.ReplaceAllString(text, "[redacted]")
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
