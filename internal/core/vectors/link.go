package vectors

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

type Link struct {
	Name       string  `json:"name"`
	Input      string  `json:"input"`
	Valid      bool    `json:"valid"`
	Canonical  bool    `json:"canonical,omitempty"`
	ID         *string `json:"id,omitempty"`
	KDFID      *int    `json:"kdf_id,omitempty"`
	S          *string `json:"s_hex,omitempty"`
	Newer      bool    `json:"newer_version,omitempty"`
	RetiredKDF bool    `json:"retired_kdf,omitempty"`
}

type linkKind int

const (
	invalidLink linkKind = iota
	canonicalLink
	validLink
	newerLink
	retiredLink
)

type linkCase struct {
	kind  linkKind
	name  string
	input string
}

func (f *File) link() error {
	id := deterministicID("link")
	secret := deterministic32("link/s")
	for _, vector := range linkCases(id, secret) {
		link := Link{
			Name:       vector.name,
			Input:      vector.input,
			Valid:      vector.kind == canonicalLink || vector.kind == validLink,
			Canonical:  vector.kind == canonicalLink,
			Newer:      vector.kind == newerLink,
			RetiredKDF: vector.kind == retiredLink,
		}
		if link.Valid {
			parsed, err := core.ParseLink(vector.input)
			if err != nil {
				return fmt.Errorf("vectors: link %q must parse: %w", vector.name, err)
			}
			link.ID = new(id.String())
			link.KDFID = new(int(parsed.KDF))
			link.S = new(hex.EncodeToString(secret[:]))
		}
		f.Link = append(f.Link, link)
	}
	return nil
}

func linkCases(id core.ID, secret *[core.SecretLen]byte) []linkCase {
	encodedID, encodedSecret := id.String(), core.Encode(secret[:])
	message := base + "/m/" + encodedID
	fragment := core.Fragment(core.KDFScrypt, secret)
	return []linkCase{
		{canonicalLink, "valid-kdf0", core.FormatLink(base, id, core.KDFNone, secret)},
		{canonicalLink, "valid-kdf1", core.FormatLink(base, id, core.KDFScrypt, secret)},
		{retiredLink, "retired-kdf2", core.FormatLink(base, id, core.KDFRetired, secret)},
		{retiredLink, "retired-kdf2-query", message + "?utm=x#1.2." + encodedSecret},
		{invalidLink, "retired-kdf2-damaged-secret", message + "#1.2." + nonCanonicalSecret(encodedSecret)},
		{invalidLink, "retired-kdf2-path-not-m", base + "/x/" + encodedID + "#1.2." + encodedSecret},
		{invalidLink, "retired-kdf2-leading-zero", message + "#1.02." + encodedSecret},
		{newerLink, "fragment-version-2", message + "#2.1." + encodedSecret},
		{newerLink, "fragment-version-2-bare", message + "#2"},
		{newerLink, "fragment-version-10-new-path-and-shape", base + "/m2/" + encodedID + "AAAA#10.x.y.z"},
		{newerLink, "fragment-version-9-digits", message + "#999999999.1." + encodedSecret},
		{invalidLink, "fragment-version-0", message + "#0.1." + encodedSecret},
		{invalidLink, "fragment-version-leading-zero", message + "#02.1." + encodedSecret},
		{invalidLink, "fragment-version-10-digits", message + "#1000000000.1." + encodedSecret},
		{invalidLink, "fragment-version-letter", message + "#v2.1." + encodedSecret},
		{invalidLink, "fragment-version-2-relative-reference", "/m/" + encodedID + "#2.1." + encodedSecret},
		{
			invalidLink,
			"fragment-version-2-userinfo",
			"https://user@nylzo.example/m/" + encodedID + "#2.1." + encodedSecret,
		},
		{invalidLink, "fragment-version-2-no-path", base + "#2.1." + encodedSecret},
		{invalidLink, "fragment-version-plus-2", message + "#+2.1." + encodedSecret},
		{invalidLink, "fragment-version-fullwidth-2", message + "#\uff12.1." + encodedSecret},
		{invalidLink, "fragment-kdf-3", message + "#1.3." + encodedSecret},
		{invalidLink, "fragment-missing", message},
		{invalidLink, "fragment-two-parts", message + "#1." + encodedSecret},
		{invalidLink, "secret-42-chars", message + "#1.1." + encodedSecret[:42]},
		{invalidLink, "secret-44-chars", message + "#1.1." + encodedSecret + "A"},
		{invalidLink, "secret-padding", message + "#1.1." + encodedSecret + "="},
		{invalidLink, "secret-non-canonical-last-char", message + "#1.1." + nonCanonicalSecret(encodedSecret)},
		{invalidLink, "secret-std-base64-alphabet", message + "#1.1." + standardAlphabet(encodedSecret[:42]) + "+"},
		{invalidLink, "id-21-chars", base + "/m/" + encodedID[:21] + "#" + fragment},
		{invalidLink, "id-23-chars", message + "A#" + fragment},
		{invalidLink, "id-non-canonical-last-char", base + "/m/" + nonCanonicalID(encodedID) + "#" + fragment},
		{invalidLink, "id-percent-encoded", base + "/m/" + percentEncodeFirst(encodedID) + "#" + fragment},
		{invalidLink, "secret-percent-encoded", message + "#1.1." + percentEncodeFirst(encodedSecret)},
		{invalidLink, "path-not-m", base + "/x/" + encodedID + "#" + fragment},
		{invalidLink, "path-trailing-slash", message + "/#" + fragment},
		{invalidLink, "relative-reference", "/m/" + encodedID + "#" + fragment},
		{invalidLink, "scheme-relative", "//nylzo.example/m/" + encodedID + "#" + fragment},
		{invalidLink, "scheme-ftp", "ftp://nylzo.example/m/" + encodedID + "#" + fragment},
		{invalidLink, "dot-segment", base + "/x/../m/" + encodedID + "#" + fragment},
		{invalidLink, "dot-segment-encoded", base + "/x/%2e%2e/m/" + encodedID + "#" + fragment},
		{invalidLink, "backslash-path", "https://nylzo.example\\m\\" + encodedID + "#" + fragment},
		{invalidLink, "userinfo", "https://user@nylzo.example/m/" + encodedID + "#" + fragment},
		{invalidLink, "leading-space", " " + message + "#" + fragment},
		{invalidLink, "trailing-space", message + "#" + fragment + " "},
		{invalidLink, "tab-in-id", base + "/m/" + encodedID[:11] + "\t" + encodedID[11:] + "#" + fragment},
		{invalidLink, "second-fragment", message + "#" + fragment + "#x"},
		{invalidLink, "double-hash", message + "##" + fragment},
		{invalidLink, "scheme-httpss", "httpss://nylzo.example/m/" + encodedID + "#" + fragment},
		{validLink, "uppercase-scheme-and-port", "HTTPS://nylzo.example:8443/m/" + encodedID + "#" + fragment},
		{validLink, "query-ignored", message + "?utm=x#" + fragment},
		{validLink, "http-localhost-port", "http://localhost:8080/m/" + encodedID + "#" + fragment},
		{validLink, "ipv6-literal", "http://[::1]:8080/m/" + encodedID + "#" + fragment},
	}
}

func nonCanonicalSecret(encoded string) string {
	return encoded[:42] + nextCharacter(encoded[42])
}

func nonCanonicalID(encoded string) string {
	return encoded[:21] + nextCharacter(encoded[21])
}

func nextCharacter(character byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	return string(alphabet[strings.IndexByte(alphabet, character)+1])
}

func percentEncodeFirst(encoded string) string {
	return fmt.Sprintf("%%%02X", encoded[0]) + encoded[1:]
}

func standardAlphabet(encoded string) string {
	return strings.NewReplacer("-", "+", "_", "/").Replace(encoded)
}
