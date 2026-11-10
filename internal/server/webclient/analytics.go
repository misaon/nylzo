package webclient

import (
	"maps"
	"slices"
)

const (
	officialPublicURL = "https://nylzo.com"
	analyticsScript   = "https://static.cloudflareinsights.com/beacon.min.js"
	analyticsEndpoint = "https://cloudflareinsights.com/cdn-cgi/rum"
	analyticsPolicy   = "nylzo-analytics"
)

// countsVisits holds for the home pages of nylzo.com, the only pages that load Cloudflare Web Analytics (D125).
func countsVisits(publicURL, template string) bool {
	return publicURL == officialPublicURL && slices.Contains(slices.Collect(maps.Values(homeTemplates)), template)
}
