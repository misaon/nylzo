package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestParseFragmentNamesNewerVersionsAndRetiredProfiles(t *testing.T) {
	t.Parallel()
	secret := core.Encode(make([]byte, core.SecretLen))
	const (
		valid = iota
		invalid
		newer
		retired
	)
	for fragment, want := range map[string]int{
		"#1.1." + secret:        valid,
		"1.0." + secret:         valid,
		"#2.1." + secret:        newer,
		"2":                     newer,
		"#7.a.b.c":              newer,
		"#1.9." + secret:        invalid,
		"#0.1." + secret:        invalid,
		"#02.1.":                invalid,
		"#-2.1.":                invalid,
		"":                      invalid,
		"#1.2." + secret:        retired,
		"1.2." + secret:         retired,
		"#1.2." + secret[:42]:   invalid,
		"#1.2." + secret + "A":  invalid,
		"#1.02." + secret:       invalid,
		"#1.2":                  invalid,
		"#1.2." + secret + ".x": invalid,
	} {
		kdf, parsed, err := core.ParseFragment(fragment)
		got := valid
		switch {
		case errors.Is(err, core.ErrLinkVersion):
			got = newer
		case errors.Is(err, core.ErrLinkRetiredKDF):
			got = retired
		case errors.Is(err, core.ErrLink):
			got = invalid
		case err != nil:
			t.Fatalf("%q: an error outside ErrLink: %v", fragment, err)
		default:
		}
		if got != want || (err != nil && (parsed != nil || kdf != core.KDFNone)) {
			t.Errorf("%q: got class %d (%v), want %d", fragment, got, err, want)
		}
	}
}

func TestDecodeExactAcceptsOnlyTheCanonicalEncoding(t *testing.T) {
	t.Parallel()
	id := core.NewID()
	encoded := id.String()
	if !strings.ContainsRune("AQgw", rune(encoded[21])) {
		t.Fatalf("a canonical id ends with %q", encoded[21])
	}
	if parsed, err := core.ParseID(encoded); err != nil || parsed != id {
		t.Fatalf("round trip: %v", err)
	}
	for _, text := range []string{
		encoded[:21] + "B",
		encoded + "=",
		encoded[:21],
		encoded + "A",
		"+" + encoded[1:],
		"/" + encoded[1:],
		encoded[:21] + "\n",
		encoded[:10] + "\r" + encoded[11:],
		encoded[:10] + " " + encoded[11:],
	} {
		if _, err := core.ParseID(text); !errors.Is(err, core.ErrEncoding) {
			t.Fatalf("%q was accepted: %v", text, err)
		}
	}
}

func FuzzParseLink(f *testing.F) {
	id := core.NewID()
	f.Add(core.FormatLink("https://nylzo.example", id, core.KDFScrypt, core.NewSecret()))
	f.Add("https://nylzo.example/m/" + id.String())
	f.Add("#1.0.")
	f.Fuzz(func(t *testing.T, raw string) {
		link, err := core.ParseLink(raw)
		if err != nil {
			return
		}
		again, err := core.ParseLink(core.FormatLink("https://nylzo.example", link.ID, link.KDF, &link.Secret))
		if err != nil || again != link {
			t.Fatal("a parsed link does not round trip")
		}
	})
}

func FuzzParseFragment(f *testing.F) {
	secret := core.NewSecret()
	f.Add(core.Fragment(core.KDFNone, secret))
	f.Add("1.2." + core.Encode(secret[:]))
	f.Fuzz(func(t *testing.T, fragment string) {
		kdf, parsed, err := core.ParseFragment(fragment)
		if err == nil && (!kdf.Valid() || parsed == nil) {
			t.Fatal("an invalid parse result")
		}
	})
}
