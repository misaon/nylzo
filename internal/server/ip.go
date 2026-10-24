package server

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
)

var forwardingHeaders = []string{"X-Forwarded-For", "X-Real-IP", "Forwarded"}

func (s *Server) clientAddr(r *http.Request) netip.Addr {
	addr, _ := s.resolveClient(r)
	return addr
}

func (s *Server) resolveClient(r *http.Request) (netip.Addr, string) {
	peer := peerAddr(r.RemoteAddr)
	switch s.cfg.ProxyMode {
	case ProxyModeScaleway:
		values := r.Header.Values("X-Real-IP")
		if len(values) == 0 {
			return peer, ""
		}
		if len(values) != 1 || (len(s.cfg.TrustedProxies) > 0 && !s.trusted(peer)) {
			return peer, "X-Real-IP"
		}
		if a, err := netip.ParseAddr(strings.TrimSpace(values[0])); err == nil {
			return a.Unmap(), ""
		}
		return peer, "X-Real-IP"
	}
	if len(s.cfg.TrustedProxies) == 0 || !s.trusted(peer) {
		for _, name := range forwardingHeaders {
			if len(r.Header.Values(name)) > 0 {
				return peer, name
			}
		}
		return peer, ""
	}
	for _, raw := range splitForwardedFor(r.Header.Values("X-Forwarded-For")) {
		a, err := netip.ParseAddr(raw)
		if err != nil {
			return peer, ""
		}
		a = a.Unmap()
		if !s.trusted(a) {
			return a, ""
		}
	}
	return peer, ""
}

func (s *Server) noticeForwarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, header := s.resolveClient(r); header != "" {
			hour := s.clock().UTC().Truncate(time.Hour).Unix()
			for last := s.forwardNotice.Load(); hour > last; last = s.forwardNotice.Load() {
				if s.forwardNotice.CompareAndSwap(last, hour) {
					s.log.Warn("forwarded-header-ignored", "header", header)
					break
				}
			}
		}
		next.ServeHTTP(w, r)
	})
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
		for part := range strings.SplitSeq(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	slices.Reverse(out)
	return out
}
