package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestParseContactKeepsPlausibleAddressesAndRefusesLinks(t *testing.T) {
	t.Parallel()
	link := core.FormatLink("https://nylzo.example", core.NewID(), core.KDFNone, core.NewSecret())
	for raw, want := range map[string]string{
		"":                                       "",
		" \t\n":                                  "",
		"  jana@example.cz\n":                    "jana@example.cz",
		"o'brien+abuse@mail.example":             "o'brien+abuse@mail.example",
		"jiří@háček.example":                     "jiří@háček.example",
		strings.Repeat("a", 243) + "@example.cz": strings.Repeat("a", 243) + "@example.cz",
	} {
		if got, err := core.ParseContact(raw); err != nil || got != want {
			t.Errorf("ParseContact(%q) = %q, %v, want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"jana",
		"@example.cz",
		"jana@",
		"jana@example",
		"jana@.example.cz",
		"jana@example..cz",
		"jana@example.cz.",
		"jana@@example.cz",
		"jana@example@cz.example",
		"ja na@example.cz",
		"jana\xc2\xa0@example.cz",
		"ja\x1bna@example.cz",
		"ja\xe2\x80\xaena@example.cz",
		"jana#1@example.cz",
		"jana@example.cz/m/x",
		"jana@exa_mple.cz",
		"ja\xffna@example.cz",
		strings.Repeat("a", 244) + "@example.cz",
		link,
		link + "@example.cz",
		"jana@example.cz " + link,
	} {
		if got, err := core.ParseContact(raw); !errors.Is(err, core.ErrContact) {
			t.Errorf("ParseContact(%q) = %q, %v, want ErrContact", raw, got, err)
		}
	}
}
