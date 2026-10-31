package loopback

import (
	"net/netip"
	"strings"
)

func Host(host string) bool {
	//nolint:gocritic // equalFold: strings.EqualFold folds Unicode and would accept "localhoſt".
	if strings.ToLower(host) == "localhost" {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && Address(address)
}

// Address excludes IPv4-mapped IPv6 addresses and zones, as the W3C Secure Contexts rule does.
func Address(address netip.Addr) bool {
	return address.Is4() && address.IsLoopback() || address == netip.IPv6Loopback()
}
