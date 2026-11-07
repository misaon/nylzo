package config

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/misaon/nylzo/internal/server/operatoraddress"
)

const maxOperatorField = 200

var (
	errNotPlainText   = errors.New("must be valid UTF-8 text of at most 200 bytes without control or format characters")
	errContactInvalid = errors.New("NYLZO_CONTACT must be an email address or an https:// address")
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
			return fmt.Errorf("%s %w", field.name, errNotPlainText)
		}
	}
	if operatoraddress.CheckContact(o.Contact) != nil {
		return errContactInvalid
	}
	return nil
}

func plainText(text string) bool {
	return len(text) <= maxOperatorField && utf8.ValidString(text) &&
		!strings.ContainsFunc(text, func(character rune) bool {
			return unicode.IsControl(character) || unicode.Is(unicode.Cf, character)
		})
}
