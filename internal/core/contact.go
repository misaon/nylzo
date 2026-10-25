package core

import (
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxContactBytes = 254

var ErrContact = errors.New("core: the contact is not a plausible email address")

func ParseContact(raw string) (string, error) {
	contact := strings.TrimSpace(raw)
	if contact == "" {
		return "", nil
	}
	local, domain, _ := strings.Cut(contact, "@")
	if len(contact) > MaxContactBytes || !utf8.ValidString(contact) || strings.Count(contact, "@") != 1 ||
		local == "" || strings.ContainsFunc(local, forbiddenInLocalPart) || !plausibleDomain(domain) {
		return "", ErrContact
	}
	return contact, nil
}

func forbiddenInLocalPart(character rune) bool {
	return character == '#' || unicode.IsSpace(character) || unicode.IsControl(character) ||
		unicode.Is(unicode.Cf, character)
}

func plausibleDomain(domain string) bool {
	labels := strings.Split(domain, ".")
	return len(labels) >= 2 && !slices.ContainsFunc(labels, func(label string) bool {
		return label == "" || strings.ContainsFunc(label, outsideDomainLabel)
	})
}

func outsideDomainLabel(character rune) bool {
	return character != '-' && !unicode.In(character, unicode.L, unicode.M, unicode.Nd)
}
