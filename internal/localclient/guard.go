package localclient

import (
	"net/http"
	"slices"
)

var fetchMetadata = []string{"Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Origin"}

// onlyThisHost refuses every name but the literal listen address, so a DNS rebinding page reaches nothing.
func (s *Server) onlyThisHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.host {
			writeJSON(w, http.StatusMisdirectedRequest, apiError{Error: "wrong_host"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// fromThisPage admits only fetch calls of a page of this origin: no navigation, no other site, no other port.
func (s *Server) fromThisPage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.sameOriginFetch(r) {
			writeJSON(w, http.StatusForbidden, apiError{Error: "cross_origin"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) sameOriginFetch(r *http.Request) bool {
	header := r.Header
	if slices.ContainsFunc(fetchMetadata, func(name string) bool { return len(header.Values(name)) > 1 }) {
		return false
	}
	if header.Get("Sec-Fetch-Site") != "same-origin" || header.Get("Sec-Fetch-Mode") == "navigate" {
		return false
	}
	if destination := header.Get("Sec-Fetch-Dest"); destination != "" && destination != "empty" {
		return false
	}
	if origin := header.Values("Origin"); len(origin) == 1 {
		return origin[0] == s.origin
	}
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}
