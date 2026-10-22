package core

import (
	"errors"
	"regexp"
	"strings"
)

var ErrLink = errors.New("core: invalid link")

const linkPathPrefix = "/m/"

type Link struct {
	ID     ID
	KDF    KDF
	Secret [SecretLen]byte
}

func Fragment(kdf KDF, s *[SecretLen]byte) string {
	return FragmentLabel + "." + kdf.String() + "." + Encode(s[:])
}

func ParseFragment(fragment string) (KDF, *[SecretLen]byte, error) {
	fragment = strings.TrimPrefix(fragment, "#")
	parts := strings.Split(fragment, ".")
	if len(parts) != 3 || parts[0] != FragmentLabel {
		return 0, nil, ErrLink
	}
	kdf, err := ParseKDF(parts[1])
	if err != nil {
		return 0, nil, ErrLink
	}
	s, err := Decode32(parts[2])
	if err != nil {
		return 0, nil, ErrLink
	}
	return kdf, s, nil
}

func FormatLink(base string, id ID, kdf KDF, s *[SecretLen]byte) string {
	return strings.TrimSuffix(base, "/") + linkPathPrefix + id.String() + "#" + Fragment(kdf, s)
}

func ParsePath(path string) (ID, error) {
	if !strings.HasPrefix(path, linkPathPrefix) {
		return ID{}, ErrLink
	}
	id, err := ParseID(strings.TrimPrefix(path, linkPathPrefix))
	if err != nil {
		return ID{}, ErrLink
	}
	return id, nil
}

var linkHead = regexp.MustCompile(`^[Hh][Tt][Tt][Pp][Ss]?://[A-Za-z0-9.:\[\]-]+(/[^?#]*)(\?[^#]*)?$`)

func ParseLink(raw string) (Link, error) {
	head, fragment, ok := strings.Cut(raw, "#")
	if !ok {
		return Link{}, ErrLink
	}
	m := linkHead.FindStringSubmatch(head)
	if m == nil {
		return Link{}, ErrLink
	}
	id, err := ParsePath(m[1])
	if err != nil {
		return Link{}, err
	}
	kdf, s, err := ParseFragment(fragment)
	if err != nil {
		return Link{}, err
	}
	return Link{ID: id, KDF: kdf, Secret: *s}, nil
}

func AssertProfile(fragmentKDF KDF, serverKDF KDF) error {
	if fragmentKDF != serverKDF {
		return ErrKDFMismatch
	}
	return nil
}
