package core

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrLink           = errors.New("core: invalid link")
	ErrLinkVersion    = fmt.Errorf("%w: a newer fragment version", ErrLink)
	ErrLinkRetiredKDF = fmt.Errorf("%w: a retired passphrase profile", ErrLink)
)

var versionLabel = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)

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
	return parseFragment(strings.TrimPrefix(fragment, "#"))
}

func parseFragment(fragment string) (KDF, *[SecretLen]byte, error) {
	if newerFragment(fragment) {
		return 0, nil, ErrLinkVersion
	}
	parts := strings.Split(fragment, ".")
	if len(parts) != 3 || parts[0] != FragmentLabel {
		return 0, nil, ErrLink
	}
	s, err := Decode32(parts[2])
	if err != nil {
		return 0, nil, ErrLink
	}
	if parts[1] == KDFRetired.String() {
		clear(s[:])
		return 0, nil, ErrLinkRetiredKDF
	}
	kdf, err := ParseKDF(parts[1])
	if err != nil {
		clear(s[:])
		return 0, nil, ErrLink
	}
	return kdf, s, nil
}

func newerFragment(fragment string) bool {
	label, _, _ := strings.Cut(fragment, ".")
	if !versionLabel.MatchString(label) {
		return false
	}
	version, err := strconv.Atoi(label)
	return err == nil && version > FragmentVersion
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
	if newerFragment(fragment) {
		return Link{}, ErrLinkVersion
	}
	id, err := ParsePath(m[1])
	if err != nil {
		return Link{}, err
	}
	kdf, s, err := parseFragment(fragment)
	if err != nil {
		return Link{}, err
	}
	return Link{ID: id, KDF: kdf, Secret: *s}, nil
}

func AssertProfile(fragmentKDF KDF, serverKDF int) error {
	if int(fragmentKDF) != serverKDF {
		return ErrKDFMismatch
	}
	return nil
}
