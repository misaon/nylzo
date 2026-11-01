package report

import (
	"errors"
	"regexp"
	"strings"
)

const MaxContactBytes = 254

var ErrContact = errors.New("core: the contact is not a plausible email address")

var (
	contactPattern = regexp.MustCompile(`^[A-Za-z0-9_%+-]+(\.[A-Za-z0-9_%+-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	keyLikeRun     = regexp.MustCompile(`[A-Za-z0-9_-]{22,}`)
)

// ParseContact admits only ASCII addresses without shell or HTML metacharacters, with IDNs as A-labels, and refuses
// a local part that holds a run of 22 or more base64url characters, which could be an id or a link key (F8).
func ParseContact(raw string) (string, error) {
	contact := strings.TrimSpace(raw)
	if contact == "" {
		return "", nil
	}
	local, _, _ := strings.Cut(contact, "@")
	if len(contact) > MaxContactBytes || !contactPattern.MatchString(contact) || keyLikeRun.MatchString(local) {
		return "", ErrContact
	}
	return contact, nil
}
