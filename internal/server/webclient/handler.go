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
	pages     Pages
	previews  map[string][]byte
	assets    map[string]Asset
	rootFiles map[string]Asset
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
		pages:     pages,
		previews:  previewPages(publicURL, build.Assets),
		assets:    build.Assets,
		rootFiles: build.RootFiles,
	}
	mux := http.NewServeMux()
	HandlePages(mux, site.page)
	mux.HandleFunc("GET "+AdminPath, site.serveAdmin)
	mux.HandleFunc("GET /", site.serveNotFound)
	mux.HandleFunc("GET /assets/{name}", site.serveAsset)
	for name := range rootFiles {
		mux.HandleFunc("GET /"+name, site.serveRootFile)
	}
	mux.HandleFunc("GET "+robotsPath, serveBody("text/plain; charset=utf-8", Robots(publicURL)))
	mux.HandleFunc("GET "+sitemapPath, serveBody("application/xml; charset=utf-8", Sitemap(publicURL)))
	securityText := SecurityText(publicURL, contact, information.Date)
	mux.HandleFunc("GET "+SecurityTextPath, serveBody("text/plain; charset=utf-8", securityText))
	mux.HandleFunc("GET "+ManifestPath, serveBody("application/json; charset=utf-8", manifest))
	return frontendHeaders(publicURL, mux), nil
}

// HandlePages routes every path of PagePaths to the handler that page returns, and a home page address
// without its trailing slash or with capitals in its tag, such as /cs or /pt-BR/, to that home page.
func HandlePages(mux *http.ServeMux, handler func(path string) http.HandlerFunc) {
	for _, path := range PagePaths {
		mux.HandleFunc("GET "+pagePattern(path), handler(path))
	}
	for from, to := range homeRedirects {
		mux.HandleFunc("GET "+pagePattern(from), func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, to, http.StatusPermanentRedirect)
		})
	}
}

func pagePattern(path string) string {
	if strings.HasSuffix(path, "/") {
		return path + "{$}"
	}
	return path
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

// serveAdmin answers with the operator's page, the one page that may ask the browser for a passkey; a build
// without it has no such address.
func (f *frontend) serveAdmin(w http.ResponseWriter, r *http.Request) {
	page, built := f.pages[adminTemplate]
	if !built {
		f.serveNotFound(w, r)
		return
	}
	w.Header().Set(permissionsPolicyHeader, adminPermissionsPolicy)
	page.Serve(w, r)
}

func (f *frontend) serveAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := f.assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	asset.Serve(w, r)
}

// serveBody answers with a document that is fixed for the process, or 404 when this instance has none.
func serveBody(contentType string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if body == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}
}

func (f *frontend) serveRootFile(w http.ResponseWriter, r *http.Request) {
	file, ok := f.rootFiles[strings.TrimPrefix(r.URL.Path, "/")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	file.Serve(w, r)
}

// No page or file of the client is a subresource of another site, so from one only a navigation may arrive.
func fromAnotherSite(r *http.Request) bool {
	site := r.Header.Get("Sec-Fetch-Site")
	return (site == "cross-site" || site == "same-site") && r.Header.Get("Sec-Fetch-Dest") != destinationDocument
}

func frontendHeaders(publicURL string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetSecurityHeaders(w.Header(), Indexable(publicURL, r.Method, r.URL.Path))
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
