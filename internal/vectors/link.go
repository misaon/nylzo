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
	ObjectHash *string `json:"h_hex,omitempty"`
	Newer      bool    `json:"newer_version,omitempty"`
	RetiredKDF bool    `json:"retired_kdf,omitempty"`
	Released   bool    `json:"released,omitempty"`
}

type linkKind int

const (
	invalidLink linkKind = iota
	canonicalLink
	validLink
	newerLink
	retiredLink
	releasedLink
)

type linkCase struct {
	kind  linkKind
	name  string
	input string
}

func (f *File) link() error {
	id := deterministicID("link")
	secret := deterministic32("link/s")
	hash := deterministic32("link/h")
	cases := append(linkCasesV2(id, secret, hash), releasedLinkCases(id, secret)...)
	for _, vector := range cases {
		link := Link{
			Name:       vector.name,
			Input:      vector.input,
			Valid:      vector.kind == canonicalLink || vector.kind == validLink,
			Canonical:  vector.kind == canonicalLink,
			Newer:      vector.kind == newerLink,
			RetiredKDF: vector.kind == retiredLink,
			Released:   vector.kind == releasedLink,
		}
		if link.Valid {
			parsed, err := core.ParseLink(vector.input)
			if err != nil {
				return fmt.Errorf("vectors: link %q must parse: %w", vector.name, err)
			}
			link.ID = new(id.String())
			link.KDFID = new(int(parsed.KDF))
			link.S = new(hex.EncodeToString(secret[:]))
			link.ObjectHash = new(hex.EncodeToString(hash[:]))
		}
		f.Link = append(f.Link, link)
	}
	return nil
}

func linkCasesV2(id core.ID, secret, hash *[32]byte) []linkCase {
	encodedID, encodedSecret, encodedHash := id.String(), core.Encode(secret[:]), core.Encode(hash[:])
	page := base + "/r"
	fields := func(kdf string) string {
		return "#2." + kdf + "." + encodedID + "." + encodedSecret + "." + encodedHash
	}
	fragment := fields("3")
	return []linkCase{
		{canonicalLink, "valid-kdf0", core.FormatLink(base, id, core.KDFNone, secret, hash)},
		{canonicalLink, "valid-kdf3", core.FormatLink(base, id, core.KDFScrypt, secret, hash)},
		{invalidLink, "kdf1-only-in-v1", page + fields("1")},
		{retiredLink, "retired-kdf2", page + fields("2")},
		{retiredLink, "retired-kdf2-query", page + "?utm=x" + fields("2")},
		{invalidLink, "retired-kdf2-damaged-hash", page + fields("2")[:len(fields("2"))-1] + "B"},
		{invalidLink, "retired-kdf2-path-m", base + "/m/" + encodedID + fields("2")},
		{invalidLink, "kdf4-reserved", page + fields("4")},
		{invalidLink, "kdf-leading-zero", page + fields("03")},
		{newerLink, "fragment-version-3", page + "#3.3." + encodedID},
		{newerLink, "fragment-version-3-bare", page + "#3"},
		{newerLink, "fragment-version-10-new-path-and-shape", base + "/q/x#10.x.y.z"},
		{newerLink, "fragment-version-9-digits", page + "#999999999.3"},
		{invalidLink, "fragment-version-0", page + "#0" + fragment[2:]},
		{invalidLink, "fragment-version-leading-zero", page + "#02" + fragment[2:]},
		{invalidLink, "fragment-version-10-digits", page + "#1000000000" + fragment[2:]},
		{invalidLink, "fragment-version-letter", page + "#v2" + fragment[2:]},
		{invalidLink, "fragment-version-3-relative-reference", "/r#3.3"},
		{invalidLink, "fragment-version-3-userinfo", "https://user@nylzo.example/r#3.3"},
		{invalidLink, "fragment-version-3-no-path", base + "#3.3"},
		{invalidLink, "fragment-version-plus-3", page + "#+3.3"},
		{invalidLink, "fragment-version-fullwidth-3", page + "#３.3"},
		{invalidLink, "fragment-missing", page},
		{invalidLink, "fragment-four-fields", page + "#2.3." + encodedID + "." + encodedSecret},
		{invalidLink, "fragment-six-fields", page + fragment + ".A"},
		{invalidLink, "id-21-chars", page + "#2.3." + encodedID[:21] + "." + encodedSecret + "." + encodedHash},
		{
			invalidLink,
			"id-non-canonical-last-char",
			page + "#2.3." + nonCanonicalID(encodedID) + "." + encodedSecret + "." + encodedHash,
		},
		{invalidLink, "secret-42-chars", page + "#2.3." + encodedID + "." + encodedSecret[:42] + "." + encodedHash},
		{
			invalidLink,
			"secret-non-canonical-last-char",
			page + "#2.3." + encodedID + "." + nonCanonicalSecret(encodedSecret) + "." + encodedHash,
		},
		{invalidLink, "hash-44-chars", page + fragment + "A"},
		{invalidLink, "hash-padding", page + fragment + "="},
		{invalidLink, "hash-non-canonical-last-char", page + fragment[:len(fragment)-1] + nonCanonicalLast(fragment)},
		{
			invalidLink,
			"hash-std-base64-alphabet",
			page + "#2.3." + encodedID + "." + encodedSecret + "." + standardAlphabet(encodedHash[:42]) + "+",
		},
		{
			invalidLink,
			"hash-percent-encoded",
			page + "#2.3." + encodedID + "." + encodedSecret + "." + percentEncodeFirst(encodedHash),
		},
		{invalidLink, "path-with-id", base + "/r/" + encodedID + fragment},
		{invalidLink, "path-m-with-id", base + "/m/" + encodedID + fragment},
		{invalidLink, "path-root", base + "/" + fragment},
		{invalidLink, "path-trailing-slash", page + "/" + fragment},
		{invalidLink, "path-uppercase", base + "/R" + fragment},
		{invalidLink, "relative-reference", "/r" + fragment},
		{invalidLink, "scheme-relative", "//nylzo.example/r" + fragment},
		{invalidLink, "scheme-ftp", "ftp://nylzo.example/r" + fragment},
		{invalidLink, "dot-segment", base + "/x/../r" + fragment},
		{invalidLink, "dot-segment-encoded", base + "/x/%2e%2e/r" + fragment},
		{invalidLink, "backslash-path", "https://nylzo.example\\r" + fragment},
		{invalidLink, "userinfo", "https://user@nylzo.example/r" + fragment},
		{invalidLink, "leading-space", " " + page + fragment},
		{invalidLink, "trailing-space", page + fragment + " "},
		{invalidLink, "second-fragment", page + fragment + "#x"},
		{invalidLink, "double-hash", page + "#" + fragment},
		{invalidLink, "scheme-httpss", "httpss://nylzo.example/r" + fragment},
		{validLink, "uppercase-scheme-and-port", "HTTPS://nylzo.example:8443/r" + fragment},
		{validLink, "query-ignored", page + "?utm=x" + fragment},
		{validLink, "http-localhost-port", "http://localhost:8080/r" + fragment},
		{validLink, "ipv6-literal", "http://[::1]:8080/r" + fragment},
	}
}

// releasedLinkCases are links of protocol version 1, whose messages the upgrade to version 2 erased.
func releasedLinkCases(id core.ID, secret *[32]byte) []linkCase {
	encodedID, encodedSecret := id.String(), core.Encode(secret[:])
	message := base + "/m/" + encodedID
	fragment := "1.1." + encodedSecret
	return []linkCase{
		{releasedLink, "v1-kdf0", message + "#1.0." + encodedSecret},
		{releasedLink, "v1-kdf1", message + "#" + fragment},
		{releasedLink, "v1-kdf2", message + "#1.2." + encodedSecret},
		{invalidLink, "v1-kdf3", message + "#1.3." + encodedSecret},
		{invalidLink, "v1-damaged-secret", message + "#1.1." + nonCanonicalSecret(encodedSecret)},
		{invalidLink, "v1-damaged-id", base + "/m/" + nonCanonicalID(encodedID) + "#" + fragment},
		{invalidLink, "v1-path-not-m", base + "/x/" + encodedID + "#" + fragment},
		{invalidLink, "v1-path-r", base + "/r#" + fragment},
		{invalidLink, "v1-fragment-two-parts", message + "#1." + encodedSecret},
		{invalidLink, "v1-secret-44-chars", message + "#" + fragment + "A"},
		{invalidLink, "v1-id-23-chars", message + "A#" + fragment},
		{invalidLink, "v1-id-percent-encoded", base + "/m/" + percentEncodeFirst(encodedID) + "#" + fragment},
		{invalidLink, "v1-path-trailing-slash", message + "/#" + fragment},
		{releasedLink, "v1-query-ignored", message + "?utm=x#" + fragment},
	}
}

func nonCanonicalSecret(encoded string) string {
	return encoded[:42] + nextCharacter(encoded[42])
}

func nonCanonicalID(encoded string) string {
	return encoded[:21] + nextCharacter(encoded[21])
}

func nonCanonicalLast(encoded string) string {
	return nextCharacter(encoded[len(encoded)-1])
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
