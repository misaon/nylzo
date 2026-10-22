package server

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

//go:embed assets/index.html
var indexHTML string

const noncePlaceholder = "__NYLZO_CSP_NONCE__"

const previewHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Nylzo</title></head><body><p>This link opens a one-time message. Open it in a browser.</p></body></html>`

var previewAgents = []string{
	"slackbot", "twitterbot", "facebookexternalhit", "facebot", "whatsapp", "telegrambot", "discordbot",
	"linkedinbot", "skypeuripreview", "applebot", "googlebot", "bingbot", "embedly", "iframely",
	"pinterestbot", "redditbot", "mastodon", "signal", "viber", "yahoo! slurp", "duckduckbot", "petalbot",
}

func isPreviewAgent(ua string) bool {
	ua = strings.ToLower(ua)
	return slices.ContainsFunc(previewAgents, func(a string) bool { return strings.Contains(ua, a) })
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id != "" {
		if _, err := core.ParseID(id); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	nonce := rand.Text()
	w.Header().Set("Content-Security-Policy", fmt.Sprintf(cspTemplate, nonce, nonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if id != "" && isPreviewAgent(r.UserAgent()) {
		w.Write([]byte(previewHTML))
		return
	}
	w.Write([]byte(strings.ReplaceAll(indexHTML, noncePlaceholder, nonce)))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeNotFound(w)
}
