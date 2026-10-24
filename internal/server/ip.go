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
	peer := peerAddr(r.RemoteAddr)
	switch s.cfg.ProxyMode {
	case ProxyModeScaleway:
		values := r.Header.Values("X-Real-IP")
		if len(values) != 1 || (len(s.cfg.TrustedProxies) > 0 && !s.trusted(peer)) {
			return peer
		}
		if a, err := netip.ParseAddr(strings.TrimSpace(values[0])); err == nil {
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

func (s *Server) noticeForwarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header := forwardingHeader(r); header != "" && s.clientAddr(r) == peerAddr(r.RemoteAddr) {
			hour := s.clock().UTC().Truncate(time.Hour).Unix()
			for last := s.forwardNotice.Load(); hour > last || hour < last-int64(time.Hour/time.Second); last = s.forwardNotice.Load() {
				if s.forwardNotice.CompareAndSwap(last, hour) {
					s.log.Warn("forwarded-header-ignored", "header", header)
					break
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func forwardingHeader(r *http.Request) string {
	for _, name := range forwardingHeaders {
		if len(r.Header.Values(name)) > 0 {
			return name
		}
	}
	return ""
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
