package core

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	FragmentVersion = 1

	fragmentLabel  = string(rune('0' + FragmentVersion))
	linkPathPrefix = "/m/"
)

var (
	ErrLink           = errors.New("core: invalid link")
	ErrLinkVersion    = fmt.Errorf("%w: a newer fragment version", ErrLink)
	ErrLinkRetiredKDF = fmt.Errorf("%w: a retired passphrase profile", ErrLink)
)

var (
	versionLabel = regexp.MustCompile(`^[1-9]\d{0,8}$`)
	linkHead     = regexp.MustCompile(`^[Hh][Tt][Tt][Pp][Ss]?://[A-Za-z0-9.:\[\]-]+(/[^?#]*)(\?[^#]*)?$`)
)

type Link struct {
	ID     ID
	KDF    KDF
	Secret [SecretLen]byte
}

func Fragment(kdf KDF, secret *[SecretLen]byte) string {
	return fragmentLabel + "." + kdf.String() + "." + Encode(secret[:])
}

func FormatLink(base string, id ID, kdf KDF, secret *[SecretLen]byte) string {
	return strings.TrimSuffix(base, "/") + linkPathPrefix + id.String() + "#" + Fragment(kdf, secret)
}

func ParseLink(raw string) (Link, error) {
	head, fragment, ok := strings.Cut(raw, "#")
	if !ok {
		return Link{}, ErrLink
	}
	match := linkHead.FindStringSubmatch(head)
	if match == nil {
		return Link{}, ErrLink
	}
	if newerFragment(fragment) {
		return Link{}, ErrLinkVersion
	}
	id, err := ParsePath(match[1])
	if err != nil {
		return Link{}, err
	}
	kdf, secret, err := parseFragmentFields(fragment)
	if err != nil {
		return Link{}, err
	}
	return Link{ID: id, KDF: kdf, Secret: *secret}, nil
}

func ParsePath(path string) (ID, error) {
	encoded, ok := strings.CutPrefix(path, linkPathPrefix)
	if !ok {
		return ID{}, ErrLink
	}
	id, err := ParseID(encoded)
	if err != nil {
		return ID{}, ErrLink
	}
	return id, nil
}

func ParseFragment(fragment string) (KDF, *[SecretLen]byte, error) {
	return parseFragmentFields(strings.TrimPrefix(fragment, "#"))
}

func AssertProfile(fragmentKDF KDF, serverKDF int) error {
	if int(fragmentKDF) != serverKDF {
		return ErrKDFMismatch
	}
	return nil
}

func parseFragmentFields(fragment string) (KDF, *[SecretLen]byte, error) {
	if newerFragment(fragment) {
		return 0, nil, ErrLinkVersion
	}
	fields := strings.Split(fragment, ".")
	if len(fields) != 3 || fields[0] != fragmentLabel {
		return 0, nil, ErrLink
	}
	secret, err := Decode32(fields[2])
	if err != nil {
		return 0, nil, ErrLink
	}
	if fields[1] == KDFRetired.String() {
		clear(secret[:])
		return 0, nil, ErrLinkRetiredKDF
	}
	kdf, err := ParseKDF(fields[1])
	if err != nil {
		clear(secret[:])
		return 0, nil, ErrLink
	}
	return kdf, secret, nil
}

func newerFragment(fragment string) bool {
	label, _, _ := strings.Cut(fragment, ".")
	if !versionLabel.MatchString(label) {
		return false
	}
	version, err := strconv.Atoi(label)
	return err == nil && version > FragmentVersion
}
