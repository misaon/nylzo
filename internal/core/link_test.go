package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestParseLinkNamesNewerVersionsRetiredProfilesAndReleasedLinks(t *testing.T) {
	t.Parallel()
	secret := core.Encode(make([]byte, core.SecretLen))
	id := "AAAAAAAAAAAAAAAAAAAAAA"
	const (
		valid = iota
		invalid
		newer
		retired
		released
	)
	v1 := "https://nylzo.example/m/" + id + "#"
	v2 := "https://nylzo.example/r#"
	fields := "." + id + "." + secret + "." + secret
	for link, want := range map[string]int{
		v1 + "1.1." + secret:            released,
		v1 + "1.0." + secret:            released,
		v1 + "1.3." + secret:            invalid,
		v1 + "3.1." + secret:            newer,
		v1 + "2":                        invalid,
		v2 + "2.0" + fields:             valid,
		v2 + "2.3" + fields:             valid,
		v2 + "2.1" + fields:             invalid,
		v2 + "2.2" + fields:             retired,
		v2 + "2.02" + fields:            invalid,
		v2 + "2.3" + fields + ".x":      invalid,
		v2 + "2.3." + id + "." + secret: invalid,
		v2 + "3":                        newer,
		v2 + "7.a.b.c":                  newer,
		v2 + "0.3" + fields:             invalid,
		v2 + "02.3":                     invalid,
		v2 + "-2.3":                     invalid,
		v2:                              invalid,
		v1 + "1.2." + secret:            released,
		v1 + "1.2." + secret[:42]:       invalid,
		v1 + "1.2." + secret + "A":      invalid,
		v1 + "1.02." + secret:           invalid,
		v1 + "1.2":                      invalid,
		v1 + "1.2." + secret + ".x":     invalid,
		"https://nylzo.example/m/" + id + "#2.3" + fields: invalid,
		"https://nylzo.example/r#1.1." + secret:           invalid,
	} {
		parsed, err := core.ParseLink(link)
		got := valid
		switch {
		case errors.Is(err, core.ErrLinkVersion):
			got = newer
		case errors.Is(err, core.ErrLinkRetiredKDF):
			got = retired
		case errors.Is(err, core.ErrLinkReleased):
			got = released
		case errors.Is(err, core.ErrLink):
			got = invalid
		case err != nil:
			t.Fatalf("%q: an error outside ErrLink: %v", link, err)
		default:
		}
		if got != want || (err != nil && parsed != (core.Link{})) {
			t.Errorf("%q: got class %d (%v), want %d", link, got, err, want)
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
	f.Add(core.FormatLink("https://nylzo.example", id, core.KDFScrypt, core.NewSecret(), new([core.ObjectHashLen]byte)))
	f.Add("https://nylzo.example/m/" + id.String() + "#1.0." + core.Encode(make([]byte, core.SecretLen)))
	f.Add("#2.0.")
	f.Fuzz(func(t *testing.T, raw string) {
		link, err := core.ParseLink(raw)
		if err != nil {
			return
		}
		formatted := core.FormatLink("https://nylzo.example", link.ID, link.KDF, &link.Secret, &link.ObjectHash)
		again, err := core.ParseLink(formatted)
		if err != nil || again != link {
			t.Fatal("a parsed link does not round trip")
		}
	})
}
