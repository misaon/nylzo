package webclient

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core"
)

func NewHandler(build Build, publicURL string, information buildinfo.Info) (http.Handler, error) {
	if publicURL != "" {
		if err := CheckPublicURL(publicURL); err != nil {
			return nil, err
		}
	}
	pages, err := build.Render(publicURL)
	if err != nil {
		return nil, err
	}
	robots, sitemap := Robots(publicURL), Sitemap(publicURL)
	manifest, err := json.Marshal(build.Manifest(information.Version, publicURL, pages, information))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	for _, path := range []string{"/{$}", "/cs/{$}", sentPath, "/transparency", "/terms", "/agents"} {
		mux.HandleFunc("GET "+path, pages.For(strings.TrimSuffix(path, "{$}")).Serve)
	}
	mux.HandleFunc("GET /cs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/cs/", http.StatusPermanentRedirect)
	})
	previews := previewPages(publicURL, build.Assets)
	readPage := func(w http.ResponseWriter, r *http.Request) {
		page := pages.For(core.LinkPath)
		if IsPreviewAgent(r.UserAgent()) {
			w.Header().Set("Content-Security-Policy", page.Policy())
			w.Header().Set("Content-Type", htmlContentType)
			w.Header().Set("Vary", "Accept-Language")
			_, _ = w.Write(previews[PreferredLanguage(r.Header.Get("Accept-Language"))])
			return
		}
		page.Serve(w, r)
	}
	notFound := func(w http.ResponseWriter, r *http.Request) {
		pages[indexTemplate].ServeStatus(w, r, http.StatusNotFound)
	}
	mux.HandleFunc("GET /", notFound)
	mux.HandleFunc("GET "+core.LinkPath, readPage)
	mux.HandleFunc("GET /m/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, parseErr := core.ParseID(r.PathValue("id")); parseErr != nil {
			notFound(w, r)
			return
		}
		readPage(w, r)
	})
	mux.HandleFunc("GET /assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		asset, ok := build.Assets[r.PathValue("name")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		asset.Serve(w, r)
	})
	mux.HandleFunc("GET /"+agentsGuideFile, serveIfBuilt(build.AgentsGuide))
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(robots)
	})
	mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		if sitemap == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write(sitemap)
	})
	mux.HandleFunc("GET "+ManifestPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(manifest)
	})
	return frontendHeaders(mux), nil
}

func serveIfBuilt(asset *Asset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if asset == nil {
			http.NotFound(w, r)
			return
		}
		asset.Serve(w, r)
	}
}

func frontendHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetSecurityHeaders(
			w.Header(),
			(r.Method == http.MethodGet || r.Method == http.MethodHead) && IsHomePath(r.URL.Path),
		)
		if r.Header.Get("Sec-Purpose") != "" && (r.URL.Path == core.LinkPath || strings.HasPrefix(r.URL.Path, "/m/")) {
			WriteUnavailable(w, time.Minute)
			return
		}
		next.ServeHTTP(w, r)
	})
}
