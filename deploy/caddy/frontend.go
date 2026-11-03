package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const transparencyPath = "/.well-known/nylzo/transparency.json"

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
	PublicURL string `json:"public_url"`
	handler   http.Handler
}

func (*Frontend) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.nylzo_frontend", New: func() caddy.Module { return new(Frontend) }}
}

func (f *Frontend) Provision(_ caddy.Context) error {
	if err := webclient.CheckPublicURL(f.PublicURL); err != nil {
		return err
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
	f.handler, err = webclient.NewHandler(build, f.PublicURL, information)
	return err
}

func (f *Frontend) UnmarshalCaddyfile(dispenser *caddyfile.Dispenser) error {
	for dispenser.Next() {
		if !dispenser.AllArgs(&f.PublicURL) || dispenser.NextBlock(0) {
			return dispenser.ArgErr()
		}
	}
	return nil
}

func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == transparencyPath || r.URL.Path == "/healthz" {
		return next.ServeHTTP(&dataResponse{ResponseWriter: w, header: make(http.Header), path: r.URL.Path}, r)
	}
	f.handler.ServeHTTP(w, r)
	return nil
}

var (
	_ caddy.Provisioner           = (*Frontend)(nil)
	_ caddyfile.Unmarshaler       = (*Frontend)(nil)
	_ caddyhttp.MiddlewareHandler = (*Frontend)(nil)
)
