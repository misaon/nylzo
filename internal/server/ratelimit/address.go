package ratelimit

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

var forwardingHeaders = []string{"X-Forwarded-For", "X-Real-IP", "Forwarded"}

type Proxies struct {
	Trusted    []netip.Prefix
	ReadRealIP bool
}

func (p Proxies) ClientAddress(r *http.Request) netip.Addr {
	peer := PeerAddress(r.RemoteAddr)
	if p.ReadRealIP {
		return p.realIP(r, peer)
	}
	if !p.trusts(peer) {
		return peer
	}
	return p.forwardedFor(r, peer)
}

func (p Proxies) realIP(r *http.Request, peer netip.Addr) netip.Addr {
	values := r.Header.Values("X-Real-IP")
	if len(values) != 1 || (len(p.Trusted) > 0 && !p.trusts(peer)) {
		return peer
	}
	address, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil {
		return peer
	}
	return address.Unmap()
}

func (p Proxies) forwardedFor(r *http.Request, peer netip.Addr) netip.Addr {
	hops := forwardedHops(r.Header.Values("X-Forwarded-For"))
	for _, hop := range slices.Backward(hops) {
		address, err := netip.ParseAddr(hop)
		if err != nil {
			return peer
		}
		if address = address.Unmap(); !p.trusts(address) {
			return address
		}
	}
	return peer
}

func (p Proxies) trusts(address netip.Addr) bool {
	return slices.ContainsFunc(p.Trusted, func(prefix netip.Prefix) bool { return prefix.Contains(address) })
}

func PeerAddress(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.IPv4Unspecified()
	}
	return address.Unmap()
}

func ForwardingHeader(r *http.Request) string {
	for _, name := range forwardingHeaders {
		if len(r.Header.Values(name)) > 0 {
			return name
		}
	}
	return ""
}

func forwardedHops(values []string) []string {
	var hops []string
	for _, value := range values {
		for hop := range strings.SplitSeq(value, ",") {
			if hop = strings.TrimSpace(hop); hop != "" {
				hops = append(hops, hop)
			}
		}
	}
	return hops
}
