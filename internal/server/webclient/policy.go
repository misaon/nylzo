package webclient

import (
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const (
	officialHost      = "nylzo.com"
	analyticsScript   = "https://static.cloudflareinsights.com/beacon.min.js"
	analyticsEndpoint = "https://cloudflareinsights.com/cdn-cgi/rum"
	analyticsPolicy   = "nylzo-analytics"
)

var (
	inlineScript    = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
	inlineStyle     = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style\s*>`)
	sourceAttribute = regexp.MustCompile(`(?i)\ssrc\s*=`)
	dataBlockType   = regexp.MustCompile(`(?i)\stype\s*=\s*"application/ld\+json"`)
)

// countsVisits holds for the home pages of nylzo.com, the only pages that load Cloudflare Web Analytics (D125).
func countsVisits(publicURL, template string) bool {
	return official(publicURL) && slices.Contains(slices.Collect(maps.Values(homeTemplates)), template)
}

// official compares origins as the browser does, so https://nylzo.com:443 is nylzo.com too.
func official(publicURL string) bool {
	parsed, err := url.Parse(publicURL)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), officialHost) &&
		(parsed.Port() == "" || parsed.Port() == "443")
}

// AnalyticsConnections lists what the home pages of an instance send visit counts to, which a front that adds its own
// connect-src must admit as well.
func AnalyticsConnections(publicURL string) []string {
	if !official(publicURL) {
		return nil
	}
	return []string{analyticsEndpoint}
}

// pagePolicy allows the inline scripts and styles of html by hash, so every response carries the same header.
func pagePolicy(html string, analytics bool) string {
	hashes := inlineHashes(html)
	var connections, trustedTypes []string
	if analytics {
		hashes.scripts = append(hashes.scripts, analyticsScript)
		connections, trustedTypes = []string{analyticsEndpoint}, []string{analyticsPolicy}
	}
	return fmt.Sprintf(
		pagePolicyFormat,
		sourceList(hashes.scripts),
		sourceList(hashes.styles),
		sourceList(connections),
		sourceList(trustedTypes),
	)
}

// StatusPolicy admits only the inline code of a status page, which loads no module, opens no connection and starts no worker.
func StatusPolicy(html string) string {
	hashes := inlineHashes(html)
	allowed := "'none'"
	if len(hashes.scripts) > 0 {
		allowed = strings.Join(hashes.scripts, " ")
	}
	return fmt.Sprintf(statusPolicyFormat, allowed, sourceList(hashes.styles))
}

type inlineCode struct {
	scripts []string
	styles  []string
}

func inlineHashes(html string) inlineCode {
	var hashes inlineCode
	for _, match := range inlineScript.FindAllStringSubmatch(html, -1) {
		if !sourceAttribute.MatchString(match[1]) && !dataBlockType.MatchString(match[1]) {
			hashes.scripts = appendHash(hashes.scripts, match[2])
		}
	}
	for _, match := range inlineStyle.FindAllStringSubmatch(html, -1) {
		hashes.styles = appendHash(hashes.styles, match[1])
	}
	return hashes
}

func appendHash(sources []string, content string) []string {
	sum := sha512.Sum384([]byte(content))
	source := "'sha384-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if slices.Contains(sources, source) {
		return sources
	}
	return append(sources, source)
}

func sourceList(sources []string) string {
	if len(sources) == 0 {
		return ""
	}
	return " " + strings.Join(sources, " ")
}
