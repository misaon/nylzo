package main

import (
	"mime"
	"net/http"
	"strings"

	"github.com/misaon/nylzo/internal/server/webclient"
)

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
		_, _ = d.ResponseWriter.Write([]byte(`{"error":"bad_gateway"}`))
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
		return len(body), nil
	}
	return d.ResponseWriter.Write(body)
}

func (d *dataResponse) FlushError() error {
	if !d.written {
		d.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(d.ResponseWriter).Flush()
}
