package webclient

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/operatoraddress"
)

type frontend struct {
	pages        Pages
	previews     map[string][]byte
	assets       map[string]Asset
	robots       []byte
	sitemap      []byte
	securityText []byte
	manifest     []byte
}

func NewHandler(build Build, publicURL, contact string, information buildinfo.Info) (http.Handler, error) {
	if publicURL != "" {
		if err := operatoraddress.CheckPublicURL(publicURL); err != nil {
			return nil, err
		}
	}
	pages, err := build.Render(publicURL, contact)
	if err != nil {
		return nil, err
	}
	manifest, _ := json.Marshal(build.Manifest(information.Version, publicURL, pages, information))
	site := &frontend{
		pages:        pages,
		previews:     previewPages(publicURL, build.Assets),
		assets:       build.Assets,
		robots:       Robots(publicURL),
		sitemap:      Sitemap(publicURL),
		securityText: SecurityText(publicURL, contact, information.Date),
		manifest:     manifest,
	}
	mux := http.NewServeMux()
	HandlePages(mux, site.page)
	mux.HandleFunc("GET /", site.serveNotFound)
	mux.HandleFunc("GET /assets/{name}", site.serveAsset)
	mux.HandleFunc("GET /"+agentsGuideFile, serveIfBuilt(build.AgentsGuide))
	mux.HandleFunc("GET "+robotsPath, site.serveRobots)
	mux.HandleFunc("GET "+sitemapPath, site.serveSitemap)
	mux.HandleFunc("GET "+SecurityTextPath, site.serveSecurityText)
	mux.HandleFunc("GET "+ManifestPath, site.serveManifest)
	return frontendHeaders(mux), nil
}

// HandlePages routes every path of PagePaths to the handler that page returns, and /cs to the Czech home page.
func HandlePages(mux *http.ServeMux, handler func(path string) http.HandlerFunc) {
	for _, path := range PagePaths {
		mux.HandleFunc("GET "+pagePattern(path), handler(path))
	}
	mux.HandleFunc("GET "+czechRedirectPath, redirectToCzechHome)
}

func pagePattern(path string) string {
	if strings.HasSuffix(path, "/") {
		return path + "{$}"
	}
	return path
}

func redirectToCzechHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, czechHome, http.StatusPermanentRedirect)
}

func (f *frontend) page(path string) http.HandlerFunc {
	switch path {
	case core.LinkPath:
		return f.serveReader
	case messagePagePath:
		return f.serveMessage
	default:
		return f.pages.For(path).Serve
	}
}

func (f *frontend) serveReader(w http.ResponseWriter, r *http.Request) {
	page := f.pages.For(core.LinkPath)
	if isPreviewAgent(r.UserAgent()) {
		w.Header().Set("Content-Security-Policy", page.Policy())
		w.Header().Set("Content-Type", htmlContentType)
		w.Header().Set("Vary", "Accept-Language")
		_, _ = w.Write(f.previews[PreferredLanguage(r.Header.Get("Accept-Language"))])
		return
	}
	page.Serve(w, r)
}

func (f *frontend) serveMessage(w http.ResponseWriter, r *http.Request) {
	if _, err := core.ParseID(r.PathValue("id")); err != nil {
		f.serveNotFound(w, r)
		return
	}
	f.serveReader(w, r)
}

func (f *frontend) serveNotFound(w http.ResponseWriter, r *http.Request) {
	f.pages[indexTemplate].ServeStatus(w, r, http.StatusNotFound)
}

func (f *frontend) serveAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := f.assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	asset.Serve(w, r)
}

func (f *frontend) serveRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(f.robots)
}

func (f *frontend) serveSitemap(w http.ResponseWriter, r *http.Request) {
	if f.sitemap == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(f.sitemap)
}

func (f *frontend) serveSecurityText(w http.ResponseWriter, r *http.Request) {
	if f.securityText == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(f.securityText)
}

func (f *frontend) serveManifest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(f.manifest)
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

// No page or file of the client is a subresource of another site, so from one only a navigation may arrive.
func fromAnotherSite(r *http.Request) bool {
	site := r.Header.Get("Sec-Fetch-Site")
	return (site == "cross-site" || site == "same-site") && r.Header.Get("Sec-Fetch-Dest") != "document"
}

func frontendHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetSecurityHeaders(
			w.Header(),
			(r.Method == http.MethodGet || r.Method == http.MethodHead) && IsHomePath(r.URL.Path),
		)
		if fromAnotherSite(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Header.Get("Sec-Purpose") != "" && (r.URL.Path == core.LinkPath || strings.HasPrefix(r.URL.Path, "/m/")) {
			WriteUnavailable(w, time.Minute)
			return
		}
		next.ServeHTTP(w, r)
	})
}
