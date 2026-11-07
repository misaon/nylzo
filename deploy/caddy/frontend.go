package main

import (
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
	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	transparencyPath = "/.well-known/nylzo/transparency.json"
	healthPath       = "/healthz"
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
	handler     http.Handler
	pages       webclient.Pages
}

func (*Frontend) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.nylzo_frontend", New: func() caddy.Module { return new(Frontend) }}
}

func (f *Frontend) Provision(_ caddy.Context) error {
	if err := webclient.CheckPublicURL(f.PublicURL); err != nil {
		return err
	}
	if f.Maintenance != "" && !filepath.IsAbs(f.Maintenance) {
		return fmt.Errorf("the maintenance file %q is not an absolute path", f.Maintenance)
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
			return errors.New("frontend build identity has no version")
		}
	}
	if f.pages, err = build.Render(f.PublicURL); err != nil {
		return err
	}
	f.handler, err = webclient.NewHandler(build, f.PublicURL, information)
	return err
}

func (f *Frontend) UnmarshalCaddyfile(dispenser *caddyfile.Dispenser) error {
	for dispenser.Next() {
		if !dispenser.Args(&f.PublicURL) || dispenser.NextArg() {
			return dispenser.ArgErr()
		}
		for dispenser.NextBlock(0) {
			if dispenser.Val() != "maintenance" {
				return dispenser.Errf("unknown subdirective %q", dispenser.Val())
			}
			if !dispenser.AllArgs(&f.Maintenance) {
				return dispenser.ArgErr()
			}
		}
	}
	return nil
}

func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	maintenance := f.underMaintenance()
	switch {
	case r.URL.Path == healthPath:
		return next.ServeHTTP(&dataResponse{ResponseWriter: w, header: make(http.Header), path: r.URL.Path}, r)
	case strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == transparencyPath:
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
