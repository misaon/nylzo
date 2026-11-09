package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/operatoraddress"
	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
)

var (
	errNotAbsolute         = errors.New("is not an absolute path")
	errBuildWithoutVersion = errors.New("frontend build identity has no version")
)

func init() {
	caddy.RegisterModule(&Frontend{})
	httpcaddyfile.RegisterHandlerDirective(
		"nylzo_frontend",
		func(helper httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
			var frontend Frontend
			return &frontend, frontend.UnmarshalCaddyfile(helper.Dispenser)
		},
	)
}

type Frontend struct {
	PublicURL   string `json:"public_url"`
	Maintenance string `json:"maintenance,omitempty"`
	// The operator's contact, the same as NYLZO_CONTACT of the server, so its link is the one the pages may reach.
	Contact         string `json:"contact,omitempty"`
	handler         http.Handler
	pages           webclient.Pages
	classicalPolicy string
}

func (*Frontend) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.nylzo_frontend", New: func() caddy.Module { return new(Frontend) }}
}

func (f *Frontend) Provision(_ caddy.Context) error {
	if err := operatoraddress.CheckPublicURL(f.PublicURL); err != nil {
		return err
	}
	if err := operatoraddress.CheckContact(f.Contact); err != nil {
		return fmt.Errorf("the contact %q: %w", f.Contact, err)
	}
	if f.Maintenance != "" && !filepath.IsAbs(f.Maintenance) {
		return fmt.Errorf("the maintenance file %q %w", f.Maintenance, errNotAbsolute)
	}
	build, err := webclient.Load(web.Files())
	if err != nil {
		return err
	}
	information := buildinfo.Info{Version: "dev"}
	if build.Built {
		if decodeErr := json.Unmarshal(build.Assets["build-info.json"].Body, &information); decodeErr != nil {
			return fmt.Errorf("frontend build identity: %w", decodeErr)
		}
		if information.Version == "" {
			return errBuildWithoutVersion
		}
	}
	f.classicalPolicy = "connect-src " + strings.Join([]string{
		f.PublicURL + "/assets/",
		f.PublicURL + api.InstancePath,
		f.PublicURL + api.TransparencyPath,
		f.PublicURL + webclient.ManifestPath,
	}, " ")
	if f.pages, err = build.Render(f.PublicURL, f.Contact); err != nil {
		return err
	}
	f.handler, err = webclient.NewHandler(build, f.PublicURL, f.Contact, information)
	return err
}

func (f *Frontend) UnmarshalCaddyfile(dispenser *caddyfile.Dispenser) error {
	for dispenser.Next() {
		if !dispenser.Args(&f.PublicURL) || dispenser.NextArg() {
			return dispenser.ArgErr()
		}
		for dispenser.NextBlock(0) {
			if err := f.unmarshalSubdirective(dispenser); err != nil {
				return err
			}
		}
	}
	return nil
}

// contactArgument reads the one optional value; an empty environment variable leaves none: no contact.
func contactArgument(dispenser *caddyfile.Dispenser) (string, error) {
	arguments := dispenser.RemainingArgs()
	if len(arguments) > 1 {
		return "", dispenser.ArgErr()
	}
	return strings.Join(arguments, ""), nil
}

func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.TLS == nil || r.TLS.CurveID != tls.X25519MLKEM768 {
		if !publicOverClassicalTLS(r) {
			webclient.SetSecurityHeaders(w.Header(), false)
			w.WriteHeader(http.StatusForbidden)
			return nil
		}
		w = &classicalResponse{ResponseWriter: w, policy: f.classicalPolicy}
	}
	maintenance := f.underMaintenance()
	switch {
	case r.URL.Path == api.HealthPath:
		return next.ServeHTTP(&dataResponse{ResponseWriter: w, header: make(http.Header), path: r.URL.Path}, r)
	case strings.HasPrefix(r.URL.Path, api.Prefix) || r.URL.Path == api.TransparencyPath:
		response := &dataResponse{ResponseWriter: w, header: make(http.Header), path: r.URL.Path}
		if maintenance {
			webclient.WriteUnavailable(response, webclient.MaintenanceRetry)
			return nil
		}
		return next.ServeHTTP(response, r)
	case maintenance && webclient.WantsPage(r):
		f.pages.ServeMaintenance(w, r)
		return nil
	default:
		f.handler.ServeHTTP(w, r)
		return nil
	}
}

// publicOverClassicalTLS admits what the public pages load; the instance limits only without message headers.
func publicOverClassicalTLS(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch r.URL.Path {
	case api.TransparencyPath:
		return true
	case api.InstancePath:
		return r.URL.RawQuery == "" && !carriesProtocolHeader(r.Header)
	default:
		return webclient.IsPublicDocument(r.URL.Path)
	}
}

func carriesProtocolHeader(header http.Header) bool {
	for name := range header {
		if strings.HasPrefix(name, "X-Nylzo-") {
			return true
		}
	}
	return false
}

// classicalResponse adds a second policy, which the browser enforces too, so the page reaches no message route.
type classicalResponse struct {
	http.ResponseWriter

	policy  string
	written bool
}

func (c *classicalResponse) WriteHeader(status int) {
	if !c.written {
		c.written = true
		c.Header().Add("Content-Security-Policy", c.policy)
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *classicalResponse) Write(body []byte) (int, error) {
	if !c.written {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(body)
}

func (c *classicalResponse) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

func (f *Frontend) unmarshalSubdirective(dispenser *caddyfile.Dispenser) error {
	switch dispenser.Val() {
	case "maintenance":
		if !dispenser.AllArgs(&f.Maintenance) {
			return dispenser.ArgErr()
		}
	case "contact":
		contact, err := contactArgument(dispenser)
		if err != nil {
			return err
		}
		f.Contact = contact
	default:
		return dispenser.Errf("unknown subdirective %q", dispenser.Val())
	}
	return nil
}

// The operator switches maintenance on and off by creating and removing the file, without restarting Caddy.
func (f *Frontend) underMaintenance() bool {
	if f.Maintenance == "" {
		return false
	}
	_, err := os.Stat(f.Maintenance)
	return err == nil
}

var (
	_ caddy.Provisioner           = (*Frontend)(nil)
	_ caddyfile.Unmarshaler       = (*Frontend)(nil)
	_ caddyhttp.MiddlewareHandler = (*Frontend)(nil)
)
