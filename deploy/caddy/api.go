package main

import (
	"errors"
	"mime"
	"net/http"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const badGatewayBody = `{"error":"bad_gateway"}`

var forwardedHeaders = []string{
	"Content-Length",
	"Retry-After",
	api.LeaseHeaderName,
	api.ShareHeaderName,
	api.PolicyHeaderName,
}

var (
	errRefusedResponse = errors.New("untrusted upstream response")
	errNoProxyResponse = errors.New("nylzo_data requires a proxy response")
)

func init() {
	caddy.RegisterModule(Data{})
	httpcaddyfile.RegisterHandlerDirective(
		"nylzo_data",
		func(helper httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
			helper.Next()
			if helper.NextArg() || helper.NextBlock(0) {
				return nil, helper.ArgErr()
			}
			return Data{}, nil
		},
	)
}

type Data struct{}

func (Data) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.nylzo_data", New: func() caddy.Module { return new(Data) }}
}

func (Data) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	replacer, ok := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	if !ok {
		return caddyhttp.Error(http.StatusInternalServerError, errNoProxyResponse)
	}
	statusValue, _ := replacer.Get("http.reverse_proxy.status_code")
	status, ok := statusValue.(int)
	if !ok {
		return caddyhttp.Error(http.StatusInternalServerError, errNoProxyResponse)
	}
	header := make(http.Header)
	for _, name := range []string{"Content-Type", "Content-Encoding"} {
		headerValue, _ := replacer.Get("http.reverse_proxy.header." + name)
		if text, isText := headerValue.(string); isText {
			header.Set(name, text)
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
	header.Set("Content-Security-Policy", webclient.NonHTMLCSP)
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
	copyForwardedHeaders(header, d.header)
	if contentType != "application/octet-stream" {
		contentType += "; charset=utf-8"
	}
	header.Set("Content-Type", contentType)
	d.ResponseWriter.WriteHeader(status)
}

func copyForwardedHeaders(destination, upstream http.Header) {
	for _, name := range forwardedHeaders {
		if value := upstream.Get(name); value != "" {
			destination.Set(name, value)
		}
	}
}

func responseContentType(path string, status int) string {
	if path == api.HealthPath {
		return "text/plain"
	}
	if status == http.StatusOK && strings.HasPrefix(path, api.MessagesPath+"/") &&
		(strings.HasSuffix(path, api.BlobAction) || strings.HasSuffix(path, api.ClaimAction)) {
		return "application/octet-stream"
	}
	return "application/json"
}

func safeDataResponse(status int, contentType string, header http.Header) bool {
	if status == http.StatusNoContent {
		return header.Get("Content-Encoding") == ""
	}
	receivedType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	return (status < 300 || status >= 400) && status != http.StatusSwitchingProtocols &&
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
