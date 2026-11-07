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
	emailContact = regexp.MustCompile(`^[A-Za-z0-9_%+-]+(\.[A-Za-z0-9_%+-]+)*@([A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+)$`)
	httpsContact = regexp.MustCompile(
		`^https://[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*(:[0-9]{1,5})?(/[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]*)?$`,
	)
	patternLiteral = regexp.MustCompile(`^[A-Za-z0-9/._~%@!$&',;=-]$`)
)

// The addresses the client links to; a link that redirects fails, because the allowlist refuses redirects.
var linkedAddresses = []string{`"https://github.com/misaon/nylzo"`, `"https://github.com/misaon/nylzo/*"`}

// CheckContact accepts no contact, an email address or an https:// address, the forms the transparency page links.
func CheckContact(contact string) error {
	if contact == "" || emailContact.MatchString(contact) || httpsContact.MatchString(contact) {
		return nil
	}
	return errors.New("want an email address or an https:// address")
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

func contactPattern(contact string) string {
	if match := emailContact.FindStringSubmatch(contact); match != nil {
		return `"mailto:*@` + match[2] + `"`
	}
	if !httpsContact.MatchString(contact) {
		return ""
	}
	parsed, err := url.Parse(contact)
	if err != nil {
		return ""
	}
	pattern := "https://" + strings.ToLower(parsed.Host) + literalPattern(parsed.EscapedPath())
	if parsed.RawQuery != "" {
		pattern += "?" + literalPattern(parsed.RawQuery)
	}
	return `"` + pattern + `"`
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
		return "", errors.New("the transparency document is not valid JSON")
	}
	if err := CheckContact(document.Operator.Contact); err != nil {
		return "", err
	}
	return document.Operator.Contact, nil
}
