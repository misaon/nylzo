package server

import (
	"crypto/rand"
	"embed"
	"io/fs"
	"net/http"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const messagePagePath = "/m/{id}"

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
	return func(server *Server) {
		if files != nil {
			server.webFiles = files
		}
	}
}

func (s *Server) loadWebClient() error {
	var err error
	if s.webBuild, err = webclient.Load(s.webFiles); err != nil {
		return err
	}
	publicURL := s.settings.PublicURL
	if s.pages, err = s.webBuild.Render(publicURL); err != nil {
		return err
	}
	s.logger.Info("web-client", "built", s.webBuild.Built, "assets", len(s.webBuild.Assets), "public_url", publicURL)
	s.robots = webclient.Robots(publicURL)
	s.sitemap = webclient.Sitemap(publicURL)
	s.manifest = s.webBuild.Manifest(s.settings.Version, publicURL)
	return nil
}

func (s *Server) pageHandler(path string) http.HandlerFunc {
	page := s.pages.For(path)
	return func(w http.ResponseWriter, r *http.Request) {
		writePage(w, r, page, startPage(w))
	}
}

// handleMessagePage serves link previews a page without script; loading the page consumes nothing.
func (s *Server) handleMessagePage(w http.ResponseWriter, r *http.Request) {
	if _, err := core.ParseID(r.PathValue("id")); err != nil {
		http.NotFound(w, r)
		return
	}
	nonce := startPage(w)
	if webclient.IsPreviewAgent(r.UserAgent()) {
		w.Write([]byte(webclient.PreviewHTML))
		return
	}
	writePage(w, r, s.pages.For(messagePagePath), nonce)
}

func startPage(w http.ResponseWriter) string {
	nonce := rand.Text()
	header := w.Header()
	header.Set("Content-Security-Policy", webclient.PageCSP(nonce))
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Vary", "Accept-Encoding")
	return nonce
}

func writePage(w http.ResponseWriter, r *http.Request, page webclient.Page, nonce string) {
	if webclient.AcceptsGzip(r.Header.Get("Accept-Encoding")) {
		w.Header().Set("Content-Encoding", webclient.EncodingGzip)
		w.Write(page.Gzip(nonce))
		return
	}
	w.Write(page.Plain(nonce))
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.webBuild.Assets[r.PathValue("name")]
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

func (s *Server) handleClientManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.manifest)
}

func handleCzechHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/cs/", http.StatusPermanentRedirect)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeNotFound(w)
}
