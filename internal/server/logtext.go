package server

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
)

var idLike = regexp.MustCompile(`[0-9a-fA-F]{32,}|[A-Za-z0-9_-]{22,}`)

func ErrorText(err error) string {
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &pathErr):
		return pathErr.Op + ": " + pathErr.Err.Error()
	case errors.As(err, &linkErr):
		return linkErr.Op + ": " + linkErr.Err.Error()
	}
	return idLike.ReplaceAllString(err.Error(), "[redacted]")
}
