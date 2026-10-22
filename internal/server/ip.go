package server

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

func (s *Server) clientAddr(r *http.Request) netip.Addr {
	peer := peerAddr(r.RemoteAddr)
	switch s.cfg.ProxyMode {
	case ProxyModeScaleway:
		if a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
			return a.Unmap()
		}
		return peer
	}
	if len(s.cfg.TrustedProxies) == 0 || !s.trusted(peer) {
		return peer
	}
	for _, raw := range splitForwardedFor(r.Header.Values("X-Forwarded-For")) {
		a, err := netip.ParseAddr(raw)
		if err != nil {
			return peer
		}
		a = a.Unmap()
		if !s.trusted(a) {
			return a
		}
	}
	return peer
}

func (s *Server) trusted(a netip.Addr) bool {
	return slices.ContainsFunc(s.cfg.TrustedProxies, func(p netip.Prefix) bool { return p.Contains(a) })
}

func peerAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.IPv4Unspecified()
	}
	return a.Unmap()
}

func splitForwardedFor(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	slices.Reverse(out)
	return out
}
