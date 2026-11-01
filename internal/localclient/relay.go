package localclient

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"

	"github.com/misaon/nylzo/internal/client"
)

const (
	nylzoHeaderPrefix = "X-Nylzo-"
	octetStream       = "application/octet-stream"
)

var (
	relayedRequestHeaders  = []string{"Authorization", "Content-Type", "Accept", "X-Requested-With"}
	relayedResponseHeaders = []string{"Content-Length", "Retry-After"}
	errRelayedRedirect     = errors.New("localclient: the instance answered with a redirect")
)

// newRelay forwards an API call as a native client would: no cookie, origin, referrer or browser metadata leaves this
// machine, and nothing the instance answers can run as code in the local origin.
func newRelay(instance *url.URL, transport http.RoundTripper) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(instance)
			request.Out.Header = kept(request.In.Header, relayedRequestHeaders)
			request.Out.Header.Set("User-Agent", client.UserAgent)
		},
		Transport:      transport,
		ModifyResponse: relayedResponse,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeJSON(w, http.StatusBadGateway, apiError{Error: "unavailable"})
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || strings.Contains(r.URL.Path, "..") {
			writeJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func relayedResponse(response *http.Response) error {
	if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
		return errRelayedRedirect
	}
	contentType := octetStream
	if response.Header.Get("Content-Type") == jsonContentType {
		contentType = jsonContentType
	}
	response.Header = kept(response.Header, relayedResponseHeaders)
	response.Header.Set("Content-Type", contentType)
	return nil
}

func kept(header http.Header, names []string) http.Header {
	out := http.Header{}
	for name, values := range header {
		if slices.Contains(names, name) || strings.HasPrefix(name, nylzoHeaderPrefix) {
			out[name] = slices.Clone(values)
		}
	}
	return out
}
