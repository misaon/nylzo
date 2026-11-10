package webclient

import (
	"regexp"
	"strings"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/operatoraddress"
)

const connectionAllowlistHeader = "Connection-Allowlist"

var patternLiteral = regexp.MustCompile(`^[A-Za-z0-9_/.~%@!&,;=-]$`)

// The addresses the client links to; a link that redirects fails, because the allowlist refuses redirects.
var linkedAddresses = []string{`"https://github.com/misaon/nylzo"`, `"https://github.com/misaon/nylzo/*"`}

// ConnectionAllowlist lets a page reach only its origin, the repository and the operator's contact, so injected
// code cannot carry a secret elsewhere by a navigation, a DNS prefetch, a redirect or WebRTC.
func ConnectionAllowlist(contact string) string {
	return pageAllowlist(contact, false)
}

func pageAllowlist(contact string, analytics bool) string {
	patterns := append([]string{"response-origin"}, linkedAddresses...)
	if pattern := contactPattern(contact); pattern != "" {
		patterns = append(patterns, pattern)
	}
	if analytics {
		patterns = append(patterns, exactPattern(analyticsScript), exactPattern(analyticsEndpoint))
	}
	return "(" + strings.Join(patterns, " ") + ")"
}

// exactPattern admits the https address with no query and no fragment.
func exactPattern(address string) string {
	path, _ := strings.CutPrefix(address, "https://")
	host, path, _ := strings.Cut(path, "/")
	return `"https://` + host + literalPattern("/"+path) + `?#"`
}

// contactPattern pins the exact address the transparency page links, with an empty or exact query and no fragment.
func contactPattern(contact string) string {
	if core.IsEmailAddress(contact) {
		return `"mailto\\:` + literalPattern(contact) + `?#"`
	}
	parsed, err := operatoraddress.ContactURL(contact)
	if err != nil {
		return ""
	}
	authority := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" && port != "443" {
		authority += ":" + port
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return `"https://` + authority + literalPattern(path) + "?" + literalPattern(parsed.RawQuery) + `#"`
}

// literalPattern escapes every URL pattern syntax character, written for a structured-field string.
func literalPattern(text string) string {
	var escaped strings.Builder
	for _, character := range text {
		if !patternLiteral.MatchString(string(character)) {
			escaped.WriteString(`\\`)
		}
		escaped.WriteRune(character)
	}
	return escaped.String()
}
