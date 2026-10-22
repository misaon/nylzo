package server

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

//go:embed assets
var assets embed.FS

const noncePlaceholder = "__NYLZO_CSP_NONCE__"

const previewHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Nylzo</title></head><body><p>This link opens a one-time message. Open it in a browser.</p></body></html>`

var previewAgents = []string{
	"slackbot", "twitterbot", "facebookexternalhit", "facebot", "whatsapp", "telegrambot", "discordbot",
	"linkedinbot", "skypeuripreview", "applebot", "googlebot", "bingbot", "embedly", "iframely",
	"pinterestbot", "redditbot", "mastodon", "signal", "viber", "yahoo! slurp", "duckduckbot", "petalbot",
}

func isPreviewAgent(ua string) bool {
	ua = strings.ToLower(ua)
	for _, a := range previewAgents {
		if strings.Contains(ua, a) {
			return true
		}
	}
	return false
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if id := r.PathValue("id"); id != "" {
		if _, err := core.ParseID(id); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	nonce := newNonce()
	w.Header().Set("Content-Security-Policy", fmt.Sprintf(cspTemplate, nonce, nonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if isPreviewAgent(r.UserAgent()) {
		w.Write([]byte(previewHTML))
		return
	}
	page, err := fs.ReadFile(assets, "assets/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	w.Write([]byte(strings.ReplaceAll(string(page), noncePlaceholder, nonce)))
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.PathValue("file"))
	if name == "index.html" || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(assets, "assets/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch path.Ext(name) {
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Write(data)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeNotFound(w)
}
