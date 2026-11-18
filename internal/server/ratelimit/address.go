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
	Trusted []netip.Prefix
}

func (p Proxies) ClientAddress(r *http.Request) netip.Addr {
	peer := PeerAddress(r.RemoteAddr)
	if !p.trusts(peer) {
		return peer
	}
	return p.forwardedFor(r, peer)
}

// FromTrustedProxy tells whether the peer of the request is one of the proxies the server trusts.
func (p Proxies) FromTrustedProxy(r *http.Request) bool {
	return p.trusts(PeerAddress(r.RemoteAddr))
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
