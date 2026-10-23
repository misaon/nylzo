package server

import (
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

//go:embed web
var webFiles embed.FS

const noncePlaceholder = "__NYLZO_CSP_NONCE__"

const previewHTML = `<!doctype html><html lang="cs"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex, nofollow"><title>Nylzo</title></head><body><p>Odkaz vede na jednorázovou zprávu. Otevřete ho v prohlížeči zařízení, ne v náhledu aplikace.</p></body></html>`

var previewAgents = []string{
	"slackbot", "twitterbot", "facebookexternalhit", "facebot", "whatsapp", "telegrambot", "discordbot",
	"linkedinbot", "skypeuripreview", "applebot", "googlebot", "bingbot", "embedly", "iframely",
	"pinterestbot", "redditbot", "mastodon", "signal", "viber", "yahoo! slurp", "duckduckbot", "petalbot",
}

var assetTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".woff2": "font/woff2",
}

type webClient struct {
	index  string
	assets map[string]webAsset
}

type webAsset struct {
	body        []byte
	contentType string
}

func WithWebFiles(files fs.FS) Option {
	return func(s *Server) {
		if files != nil {
			s.webFiles = files
		}
	}
}

func loadWebClient(files fs.FS) (webClient, error) {
	index, err := fs.ReadFile(files, "web/dist/index.html")
	if errors.Is(err, fs.ErrNotExist) {
		placeholder, err := fs.ReadFile(files, "web/placeholder.html")
		if err != nil {
			return webClient{}, fmt.Errorf("server: web placeholder: %w", err)
		}
		return webClient{index: string(placeholder)}, nil
	}
	if err != nil {
		return webClient{}, fmt.Errorf("server: web client: %w", err)
	}
	client := webClient{index: string(index), assets: map[string]webAsset{}}
	entries, err := fs.ReadDir(files, "web/dist/assets")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return webClient{}, fmt.Errorf("server: web assets: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return webClient{}, fmt.Errorf("server: web asset %s is a directory", entry.Name())
		}
		contentType, ok := assetTypes[path.Ext(entry.Name())]
		if !ok {
			return webClient{}, fmt.Errorf("server: web asset %s has no known type", entry.Name())
		}
		body, err := fs.ReadFile(files, "web/dist/assets/"+entry.Name())
		if err != nil {
			return webClient{}, fmt.Errorf("server: web asset %s: %w", entry.Name(), err)
		}
		client.assets[entry.Name()] = webAsset{body: body, contentType: contentType}
	}
	return client, nil
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
	w.Write([]byte(strings.ReplaceAll(s.web.index, noncePlaceholder, nonce)))
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.web.assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", asset.contentType)
	w.Write(asset.body)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeNotFound(w)
}
