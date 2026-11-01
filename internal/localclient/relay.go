package localclient

import (
	"io"
	"maps"
	"net/http"
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
	errUnavailable         = apiError{Error: "unavailable"}
)

// relay sends an API call as a native client would, and passes back only a final answer whose headers cannot make
// the local origin run anything: no informational answer, upgrade, redirect, trailer, cookie or policy.
type relay struct {
	instance  *url.URL
	transport http.RoundTripper
}

func newRelay(instance *url.URL, transport http.RoundTripper) http.Handler {
	return relay{instance: instance, transport: transport}
}

func (rl relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || strings.Contains(r.URL.Path, "..") {
		writeJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	response, ok := rl.forward(r)
	if !ok {
		writeJSON(w, http.StatusBadGateway, errUnavailable)
		return
	}
	defer response.Body.Close()
	contentType := octetStream
	if response.Header.Get("Content-Type") == jsonContentType {
		contentType = jsonContentType
	}
	maps.Copy(w.Header(), kept(response.Header, relayedResponseHeaders))
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (rl relay) forward(r *http.Request) (*http.Response, bool) {
	body := r.Body
	if r.ContentLength == 0 {
		body = http.NoBody
	}
	target := *rl.instance
	target.Path = r.URL.Path
	outgoing, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), body)
	if err != nil {
		return nil, false
	}
	outgoing.ContentLength = r.ContentLength
	outgoing.Header = kept(r.Header, relayedRequestHeaders)
	outgoing.Header.Set("User-Agent", client.UserAgent)
	response, err := rl.transport.RoundTrip(outgoing)
	if err != nil {
		return nil, false
	}
	final := response.StatusCode >= http.StatusOK &&
		(response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest)
	if !final {
		response.Body.Close()
		return nil, false
	}
	return response, true
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
