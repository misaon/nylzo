package crossorigin

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/misaon/nylzo/internal/loopback"
)

const (
	requestedWith = "XMLHttpRequest"
	headerReferer = "Referer"
	schemeHTTP    = "http"
	schemeHTTPS   = "https"
)

type browserMetadata struct {
	present bool
	valid   bool
}

func Guard(publicURL string, refuse, next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if !sameOriginOrDirect(r.Header) {
			refuse.ServeHTTP(w, r)
			return
		}
		if safeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if !validBrowserMutation(r, publicURL, protection) {
			refuse.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOriginOrDirect is the Fetch Metadata resource isolation policy: an image or a link on another site must not
// spend its visitors' read budget (F2). A request without Sec-Fetch-Site comes from a native client.
func sameOriginOrDirect(header http.Header) bool {
	site := header.Values("Sec-Fetch-Site")
	return len(site) == 0 || len(site) == 1 && (site[0] == "same-origin" || site[0] == "none")
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func validBrowserMutation(r *http.Request, publicURL string, protection *http.CrossOriginProtection) bool {
	metadata := browserRequestContext(r.Header)
	if !metadata.valid || !validRequestMarker(r.Header, metadata.present) {
		return false
	}
	// Native clients have no ambient browser credentials. Older clients already
	// require X-Nylzo-Create or a signed Authorization header in their handlers.
	if !metadata.present {
		return true
	}
	target, ok := mutationTarget(r, publicURL)
	if !ok || !matchingSourceOrigins(r.Header, target) {
		return false
	}
	// Compare with the configured public target, not a peer-supplied Host or
	// forwarding header. The original request is not modified.
	probe := *r
	probe.Host = strings.TrimPrefix(strings.TrimPrefix(target, "https://"), "http://")
	if r.Header.Get("Origin") != "" {
		probe.Header = r.Header.Clone()
		probe.Header.Set("Origin", target)
	}
	return protection.Check(&probe) == nil
}

func browserRequestContext(header http.Header) browserMetadata {
	browser := false
	for _, name := range []string{"Origin", headerReferer, "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest"} {
		values := header.Values(name)
		if len(values) > 1 || len(values) == 1 && values[0] == "" {
			return browserMetadata{}
		}
		browser = browser || len(values) != 0
	}
	return browserMetadata{present: browser, valid: true}
}

func validRequestMarker(header http.Header, browser bool) bool {
	markers := header.Values("X-Requested-With")
	return len(markers) <= 1 && (len(markers) != 1 || markers[0] == requestedWith) && (!browser || len(markers) == 1)
}

func matchingSourceOrigins(header http.Header, target string) bool {
	for _, name := range []string{"Origin", headerReferer} {
		if value := header.Get(name); value != "" {
			origin, valid := sourceOrigin(value, name == headerReferer)
			if !valid || origin != target {
				return false
			}
		}
	}
	return true
}

func mutationTarget(r *http.Request, publicURL string) (string, bool) {
	if publicURL != "" {
		return sourceOrigin(publicURL, false)
	}
	scheme := schemeHTTP
	if r.TLS != nil {
		scheme = schemeHTTPS
	}
	target, ok := sourceOrigin(scheme+"://"+r.Host, false)
	parsed, err := url.Parse(target)
	if !ok || err != nil {
		return "", false
	}
	// Unconfigured local development remains usable without admitting DNS rebinding.
	return target, loopback.Host(parsed.Hostname())
}

func sourceOrigin(value string, referer bool) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || !validSourceURL(parsed, referer) {
		return "", false
	}
	host := strings.ToLower(parsed.Host)
	if parsed.Scheme == schemeHTTPS && parsed.Port() == "443" || parsed.Scheme == schemeHTTP && parsed.Port() == "80" {
		host = strings.ToLower(parsed.Hostname())
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	return parsed.Scheme + "://" + host, true
}

func validSourceURL(parsed *url.URL, referer bool) bool {
	return parsed.User == nil && parsed.Host != "" && parsed.Fragment == "" &&
		(parsed.Scheme == schemeHTTPS || parsed.Scheme == schemeHTTP) &&
		(referer || parsed.Path == "" && parsed.RawQuery == "" && !parsed.ForceQuery)
}
