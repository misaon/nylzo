package web

import (
	"embed"
	"io/fs"
)

//go:embed all:*
var files embed.FS

func Files() fs.FS {
	return files
}
