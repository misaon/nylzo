package main

import (
	"errors"
	"mime"
	"net/http"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/misaon/nylzo/internal/server/webclient"
)

const badGatewayBody = `{"error":"bad_gateway"}`

var errRefusedResponse = errors.New("untrusted upstream response")

func init() {
	caddy.RegisterModule(Data{})
	httpcaddyfile.RegisterHandlerDirective("nylzo_data", func(helper httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		helper.Next()
		if helper.NextArg() || helper.NextBlock(0) {
			return nil, helper.ArgErr()
		}
		return Data{}, nil
	})
}

type Data struct{}

func (Data) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.nylzo_data", New: func() caddy.Module { return new(Data) }}
}

func (Data) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	replacer := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	value, _ := replacer.Get("http.reverse_proxy.status_code")
	status, ok := value.(int)
	if !ok {
		return caddyhttp.Error(http.StatusInternalServerError, errors.New("nylzo_data requires a proxy response"))
	}
	header := make(http.Header)
	for _, name := range []string{"Content-Type", "Content-Encoding"} {
		if value, ok := replacer.Get("http.reverse_proxy.header." + name); ok {
			header.Set(name, value.(string))
		}
	}
	if !safeDataResponse(status, responseContentType(r.URL.Path, status), header) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, err := w.Write([]byte(badGatewayBody))
		return err
	}
	return next.ServeHTTP(w, r)
}

// Unwrap would let an upstream protocol upgrade bypass this response boundary.
type dataResponse struct {
	http.ResponseWriter

	header  http.Header
	path    string
	written bool
	refused bool
}

func (d *dataResponse) Header() http.Header {
	return d.header
}

func (d *dataResponse) WriteHeader(status int) {
	if d.written || status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		return
	}
	d.written = true
	contentType := responseContentType(d.path, status)
	d.refused = !safeDataResponse(status, contentType, d.header)
	header := d.ResponseWriter.Header()
	clear(header)
	webclient.SetSecurityHeaders(header, false)
	header.Set(
		"Content-Security-Policy",
		"default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; sandbox",
	)
	if d.refused {
		header.Set("Content-Type", "application/json; charset=utf-8")
		d.ResponseWriter.WriteHeader(http.StatusBadGateway)
		_, _ = d.ResponseWriter.Write([]byte(badGatewayBody))
		return
	}
	if status == http.StatusNoContent {
		// HTTP/3 otherwise adds Content-Length: 0 when the handler finishes.
		header["Content-Length"] = nil
		d.ResponseWriter.WriteHeader(status)
		return
	}
	for _, name := range []string{"Content-Length", "Retry-After", "X-Nylzo-Lease", "X-Nylzo-Share", "X-Nylzo-Policy"} {
		if value := d.header.Get(name); value != "" {
			header.Set(name, value)
		}
	}
	if contentType != "application/octet-stream" {
		contentType += "; charset=utf-8"
	}
	header.Set("Content-Type", contentType)
	d.ResponseWriter.WriteHeader(status)
}

func responseContentType(path string, status int) string {
	if path == "/healthz" {
		return "text/plain"
	}
	if status == http.StatusOK && strings.HasPrefix(path, "/api/v1/messages/") &&
		(strings.HasSuffix(path, "/blob") || strings.HasSuffix(path, "/claim")) {
		return "application/octet-stream"
	}
	return "application/json"
}

func safeDataResponse(status int, contentType string, header http.Header) bool {
	if status == http.StatusNoContent {
		return header.Get("Content-Encoding") == ""
	}
	receivedType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	return !(status >= 300 && status < 400) && status != http.StatusSwitchingProtocols &&
		err == nil && receivedType == contentType && len(header.Values("Content-Type")) == 1 &&
		header.Get("Content-Encoding") == ""
}

func (d *dataResponse) Write(body []byte) (int, error) {
	if !d.written {
		d.WriteHeader(http.StatusOK)
	}
	if d.refused {
		return 0, errRefusedResponse
	}
	return d.ResponseWriter.Write(body)
}

func (d *dataResponse) FlushError() error {
	if !d.written {
		d.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(d.ResponseWriter).Flush()
}
