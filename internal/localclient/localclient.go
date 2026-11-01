package localclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/loopback"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	DefaultAddress    = "127.0.0.1:0"
	HandoffPath       = "/open"
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = time.Minute
	maxHeaderBytes    = 16 << 10
	jsonContentType   = "application/json; charset=utf-8"
)

var (
	ErrNotLoopback = errors.New("localclient: the verified client listens only on 127.0.0.1 or [::1]")
	errNoClient    = errors.New("localclient: this nylzo carries no web client; use a release build")
)

type Notice int

const (
	HandoffTaken Notice = iota + 1
	HandoffRefused
)

type Config struct {
	Instance  string
	Build     webclient.Build
	Version   string
	BuildInfo buildinfo.Info
	Transport http.RoundTripper
	Notify    func(Notice)
}

type Server struct {
	host     string
	origin   string
	instance string
	pages    webclient.Pages
	assets   map[string]webclient.Asset
	manifest webclient.Manifest
	relay    http.Handler
	handoff  handoff
	notify   func(Notice)
	handler  http.Handler
}

// Listen accepts only a literal loopback address, so no name lookup can move the client off this machine.
func Listen(ctx context.Context, address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrNotLoopback
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr != nil || !loopback.Address(ip) {
		return nil, ErrNotLoopback
	}
	var config net.ListenConfig
	return config.Listen(ctx, "tcp", address)
}

func New(address net.Addr, config Config) (*Server, error) {
	listening, ok := address.(*net.TCPAddr)
	if !ok {
		return nil, ErrNotLoopback
	}
	bound := netip.AddrPortFrom(listening.AddrPort().Addr().Unmap(), listening.AddrPort().Port())
	if !loopback.Address(bound.Addr()) {
		return nil, ErrNotLoopback
	}
	instance, err := client.Origin(config.Instance)
	if err != nil {
		return nil, err
	}
	if !config.Build.Built {
		return nil, errNoClient
	}
	pages, err := config.Build.Render(instance)
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(instance)
	if err != nil {
		return nil, err
	}
	transport := config.Transport
	if transport == nil {
		transport = client.NewTransport(instance)
	}
	host := bound.String()
	local := &Server{
		host:     host,
		origin:   "http://" + host,
		instance: instance,
		pages:    pages,
		assets:   config.Build.Assets,
		manifest: config.Build.Manifest(config.Version, instance, pages, config.BuildInfo),
		relay:    newRelay(target, transport),
		notify:   config.Notify,
	}
	if local.notify == nil {
		local.notify = func(Notice) {}
	}
	local.handler = local.routes()
	return local, nil
}

func (s *Server) Origin() string {
	return s.origin
}

func (s *Server) Instance() string {
	return s.instance
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	for _, path := range webclient.PagePaths {
		mux.HandleFunc("GET "+pattern(path), s.page(path))
	}
	mux.HandleFunc("GET /cs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/cs/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET "+HandoffPath, s.pages.For(HandoffPath).Serve)
	mux.Handle("POST "+HandoffPath, s.fromThisPage(http.HandlerFunc(s.handleRedeem)))
	mux.HandleFunc("GET /assets/{name}", s.handleAsset)
	mux.HandleFunc("GET "+webclient.ManifestPath, s.handleManifest)
	mux.Handle("/api/v1/", s.fromThisPage(s.relay))
	mux.Handle("GET "+server.TransparencyPath, s.fromThisPage(s.relay))
	return securityHeaders(s.onlyThisHost(mux))
}

func pattern(path string) string {
	if strings.HasSuffix(path, "/") {
		return path + "{$}"
	}
	return path
}

func (s *Server) page(path string) http.HandlerFunc {
	page := s.pages.For(path)
	if !strings.Contains(path, "{id}") {
		return page.Serve
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := core.ParseID(r.PathValue("id")); err != nil {
			http.NotFound(w, r)
			return
		}
		page.Serve(w, r)
	}
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	asset.Serve(w, r)
}

func (s *Server) handleManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.manifest)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webclient.SetSecurityHeaders(w.Header(), false)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		status, body = http.StatusInternalServerError, []byte(`{"error":"internal"}`)
	}
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	w.Write(body)
}

type apiError struct {
	Error string `json:"error"`
}
