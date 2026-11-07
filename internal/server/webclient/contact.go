package webclient

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const connectionAllowlistHeader = "Connection-Allowlist"

var (
	emailContact   = regexp.MustCompile(`^[A-Za-z0-9_%+-]+(\.[A-Za-z0-9_%+-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	httpsContact   = regexp.MustCompile(`^https://[A-Za-z0-9_.~:/?@!&+,;=%-]+$`)
	patternLiteral = regexp.MustCompile(`^[A-Za-z0-9_/.~%@!&,;=-]$`)
)

var (
	errContact = errors.New(
		"want an email address or an https:// address without user, fragment or dot segments",
	)
	errTransparencyNotJSON = errors.New("the transparency document is not valid JSON")
)

// The addresses the client links to; a link that redirects fails, because the allowlist refuses redirects.
var linkedAddresses = []string{`"https://github.com/misaon/nylzo"`, `"https://github.com/misaon/nylzo/*"`}

// CheckContact accepts no contact, an email address or an https:// address, the forms the transparency page links.
func CheckContact(contact string) error {
	if contact == "" || emailContact.MatchString(contact) {
		return nil
	}
	_, err := contactURL(contact)
	return err
}

func contactURL(contact string) (*url.URL, error) {
	parsed, err := url.Parse(contact)
	if err != nil || !httpsContact.MatchString(contact) || parsed.Scheme != "https" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.Host == "" || strings.HasSuffix(parsed.Host, ":") ||
		parsed.Fragment != "" || parsed.ForceQuery {
		return nil, errContact
	}
	if _, queryErr := url.QueryUnescape(parsed.RawQuery); queryErr != nil {
		return nil, errContact
	}
	if segments := "/" + parsed.EscapedPath() + "/"; strings.Contains(segments, "/./") ||
		strings.Contains(segments, "/../") {
		return nil, errContact
	}
	if portErr := checkPort(parsed.Port()); portErr != nil {
		return nil, portErr
	}
	if hostErr := checkHost(strings.ToLower(parsed.Hostname())); hostErr != nil {
		return nil, hostErr
	}
	return parsed, nil
}

// ConnectionAllowlist lets a page reach only its origin, the repository and the operator's contact, so injected
// code cannot carry a secret elsewhere by a navigation, a DNS prefetch, a redirect or WebRTC.
func ConnectionAllowlist(contact string) string {
	patterns := append([]string{"response-origin"}, linkedAddresses...)
	if pattern := contactPattern(contact); pattern != "" {
		patterns = append(patterns, pattern)
	}
	return "(" + strings.Join(patterns, " ") + ")"
}

// contactPattern pins the exact address the transparency page links, with an empty or exact query and no fragment.
func contactPattern(contact string) string {
	if emailContact.MatchString(contact) {
		return `"mailto\\:` + literalPattern(contact) + `?#"`
	}
	parsed, err := contactURL(contact)
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

// OperatorContact reads the operator's contact from an instance's transparency document.
func OperatorContact(transparency []byte) (string, error) {
	var document struct {
		Operator struct {
			Contact string `json:"contact"`
		} `json:"operator"`
	}
	if err := json.Unmarshal(transparency, &document); err != nil {
		return "", errTransparencyNotJSON
	}
	if err := CheckContact(document.Operator.Contact); err != nil {
		return "", err
	}
	return document.Operator.Contact, nil
}
