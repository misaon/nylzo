package server

import (
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
	s.manifest = s.webBuild.Manifest(s.settings.Version, publicURL, s.pages, s.buildInformation())
	return nil
}

func (s *Server) pageHandler(path string) http.HandlerFunc {
	return s.pages.For(path).Serve
}

// handleMessagePage serves the page that tells a holder of a protocol version 1 link that its message is gone.
func (s *Server) handleMessagePage(w http.ResponseWriter, r *http.Request) {
	if _, err := core.ParseID(r.PathValue("id")); err != nil {
		http.NotFound(w, r)
		return
	}
	servePreviewOr(w, r, s.pages.For(messagePagePath))
}

// handleReadPage serves every reader the same page and link previews a page without script.
func (s *Server) handleReadPage(w http.ResponseWriter, r *http.Request) {
	servePreviewOr(w, r, s.pages.For(core.LinkPath))
}

func servePreviewOr(w http.ResponseWriter, r *http.Request, page webclient.Page) {
	if webclient.IsPreviewAgent(r.UserAgent()) {
		w.Header().Set("Content-Security-Policy", page.Policy())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(webclient.PreviewHTML))
		return
	}
	page.Serve(w, r)
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.webBuild.Assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	asset.Serve(w, r)
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
