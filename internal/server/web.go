package server

import (
	"io/fs"
	"net/http"

	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
)

func EmbeddedWebClient() (webclient.Build, error) {
	return webclient.Load(web.Files())
}

func WithWebFiles(files fs.FS) Option {
	return func(server *Server) {
		if files != nil {
			server.webFiles = files
		}
	}
}

func (s *Server) loadWebClient() error {
	build, err := webclient.Load(s.webFiles)
	if err != nil {
		return err
	}
	if s.frontend, err = webclient.NewHandler(
		build,
		s.settings.PublicURL,
		s.settings.Operator.Contact,
		s.buildInformation(),
	); err != nil {
		return err
	}
	s.logger.Info("web-client", "built", build.Built, "assets", len(build.Assets), "public_url", s.settings.PublicURL)
	return nil
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeNotFound(w)
}
