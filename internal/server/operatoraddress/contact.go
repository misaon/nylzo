package operatoraddress

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

var httpsContact = regexp.MustCompile(`^https://[A-Za-z0-9_.~:/?@!&+,;=%-]+$`)

var (
	errContact = errors.New(
		"want an email address or an https:// address without user, fragment or dot segments",
	)
	errTransparencyNotJSON = errors.New("the transparency document is not valid JSON")
)

// CheckContact accepts no contact, an email address or an https:// address, the forms the transparency page links.
func CheckContact(contact string) error {
	if contact == "" || core.IsEmailAddress(contact) {
		return nil
	}
	_, err := ContactURL(contact)
	return err
}

func ContactURL(contact string) (*url.URL, error) {
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
