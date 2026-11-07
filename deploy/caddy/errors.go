package main

import (
	"errors"
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
)

func init() {
	caddy.RegisterModule(&ErrorPage{})
	httpcaddyfile.RegisterHandlerDirective(
		"nylzo_error",
		func(helper httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
			var page ErrorPage
			return &page, page.UnmarshalCaddyfile(helper.Dispenser)
		},
	)
}

// ErrorPage answers inside handle_errors: a browser that navigated gets the server error page, anything else the bare status.
type ErrorPage struct {
	pages webclient.Pages
}

func (*ErrorPage) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.nylzo_error", New: func() caddy.Module { return new(ErrorPage) }}
}

func (e *ErrorPage) Provision(_ caddy.Context) error {
	build, err := webclient.Load(web.Files())
	if err != nil {
		return err
	}
	e.pages, err = build.Render("", "")
	return err
}

func (*ErrorPage) UnmarshalCaddyfile(dispenser *caddyfile.Dispenser) error {
	for dispenser.Next() {
		if dispenser.NextArg() || dispenser.NextBlock(0) {
			return dispenser.ArgErr()
		}
	}
	return nil
}

func (e *ErrorPage) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	replacer, ok := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	if !ok {
		return errors.New("nylzo_error requires a request replacer")
	}
	value, _ := replacer.Get("http.error.status_code")
	status, ok := value.(int)
	if !ok {
		return errors.New("nylzo_error runs only inside handle_errors")
	}
	if status >= http.StatusInternalServerError {
		e.pages.ServeServerError(w, r, status)
		return nil
	}
	webclient.SetSecurityHeaders(w.Header(), false)
	w.WriteHeader(status)
	return nil
}

var (
	_ caddy.Provisioner           = (*ErrorPage)(nil)
	_ caddyfile.Unmarshaler       = (*ErrorPage)(nil)
	_ caddyhttp.MiddlewareHandler = (*ErrorPage)(nil)
)
