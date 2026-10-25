package config

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxOperatorField = 200

var (
	emailContact = regexp.MustCompile(`^[A-Za-z0-9_%+-]+(\.[A-Za-z0-9_%+-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	httpsContact = regexp.MustCompile(
		`^https://[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*(:[0-9]{1,5})?(/[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]*)?$`,
	)
)

type Operator struct {
	Name    string `json:"name,omitempty"`
	Contact string `json:"contact,omitempty"`
	Hosting string `json:"hosting,omitempty"`
}

func (o Operator) validate() error {
	for _, field := range []struct{ name, value string }{
		{"NYLZO_OPERATOR", o.Name}, {"NYLZO_CONTACT", o.Contact}, {"NYLZO_HOSTING", o.Hosting},
	} {
		if !plainText(field.value) {
			return errors.New(
				field.name + " must be valid UTF-8 text of at most 200 bytes without control or format characters",
			)
		}
	}
	if o.Contact != "" && !emailContact.MatchString(o.Contact) && !httpsContact.MatchString(o.Contact) {
		return errors.New("NYLZO_CONTACT must be an email address or an https:// address")
	}
	return nil
}

func plainText(text string) bool {
	return len(text) <= maxOperatorField && utf8.ValidString(text) &&
		!strings.ContainsFunc(text, func(character rune) bool {
			return unicode.IsControl(character) || unicode.Is(unicode.Cf, character)
		})
}
