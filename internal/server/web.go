package server

import (
	"crypto/rand"
	"embed"
	"io/fs"
	"net/http"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/webclient"
)

//go:embed web
var embeddedWeb embed.FS

func EmbeddedWebClient() (webclient.Build, error) {
	files, err := embeddedWebFiles()
	if err != nil {
		return webclient.Build{}, err
	}
	return webclient.Load(files)
}

func embeddedWebFiles() (fs.FS, error) {
	return fs.Sub(embeddedWeb, "web")
}

func WithWebFiles(files fs.FS) Option {
	return func(s *Server) {
		if files != nil {
			s.webFiles = files
		}
	}
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
	header := w.Header()
	header.Set("Content-Security-Policy", webclient.PageCSP(nonce))
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Vary", "Accept-Encoding")
	if id != "" && webclient.IsPreviewAgent(r.UserAgent()) {
		w.Write([]byte(webclient.PreviewHTML))
		return
	}
	page := s.pages.For(r.URL.Path)
	if webclient.AcceptsGzip(r.Header.Get("Accept-Encoding")) {
		header.Set("Content-Encoding", webclient.EncodingGzip)
		w.Write(page.Gzip(nonce))
		return
	}
	w.Write(page.Plain(nonce))
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.web.Assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	header := w.Header()
	header.Set("Content-Type", asset.ContentType)
	header.Set("Vary", "Accept-Encoding")
	if len(asset.Gzip) > 0 && webclient.AcceptsGzip(r.Header.Get("Accept-Encoding")) {
		header.Set("Content-Encoding", webclient.EncodingGzip)
		w.Write(asset.Gzip)
		return
	}
	w.Write(asset.Body)
}

func (s *Server) handleRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(s.robots)
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	if s.sitemap == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write(s.sitemap)
}

func (s *Server) handleCzechHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/cs/", http.StatusPermanentRedirect)
}

func (s *Server) handleClientManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.manifest)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeNotFound(w)
}
