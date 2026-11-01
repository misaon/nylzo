package core

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	FragmentVersion = 2
	LinkPath        = "/r"
)

var (
	ErrLink           = errors.New("core: invalid link")
	ErrLinkVersion    = fmt.Errorf("%w: a newer fragment version", ErrLink)
	ErrLinkRetiredKDF = fmt.Errorf("%w: a retired passphrase profile", ErrLink)
	ErrLinkReleased   = fmt.Errorf(
		"%w: a link from before protocol version 2, whose message the upgrade erased",
		ErrLink,
	)
)

var (
	versionLabel = regexp.MustCompile(`^[1-9]\d{0,8}$`)
	linkHead     = regexp.MustCompile(`^[Hh][Tt][Tt][Pp][Ss]?://[A-Za-z0-9.:\[\]-]+(/[^?#]*)(\?[^#]*)?$`)
)

type Link struct {
	ID         ID
	KDF        KDF
	Secret     [SecretLen]byte
	ObjectHash [ObjectHashLen]byte
}

func (l *Link) Wipe() {
	clear(l.Secret[:])
}

func FormatLink(base string, id ID, kdf KDF, secret *[SecretLen]byte, objectHash *[ObjectHashLen]byte) string {
	fragment := strings.Join([]string{
		strconv.Itoa(FragmentVersion),
		kdf.String(),
		id.String(),
		Encode(secret[:]),
		Encode(objectHash[:]),
	}, ".")
	return strings.TrimSuffix(base, "/") + LinkPath + "#" + fragment
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
	label, _, _ := strings.Cut(fragment, ".")
	switch {
	case label == strconv.Itoa(FragmentVersion):
		return parseFragment(match[1], fragment)
	case releasedLink(match[1], fragment):
		return Link{}, ErrLinkReleased
	default:
		return Link{}, ErrLink
	}
}

func AssertProfile(fragmentKDF KDF, serverKDF int) error {
	if int(fragmentKDF) != serverKDF {
		return ErrKDFMismatch
	}
	return nil
}

func parseFragment(path, fragment string) (Link, error) {
	if path != LinkPath {
		return Link{}, ErrLink
	}
	fields := strings.Split(fragment, ".")
	if len(fields) != 5 {
		return Link{}, ErrLink
	}
	var link Link
	id, err := ParseID(fields[2])
	if err != nil {
		return Link{}, ErrLink
	}
	link.ID = id
	if !decodeInto(link.Secret[:], fields[3]) || !decodeInto(link.ObjectHash[:], fields[4]) {
		link.Wipe()
		return Link{}, ErrLink
	}
	if kdfErr := link.setKDF(fields[1]); kdfErr != nil {
		return Link{}, kdfErr
	}
	return link, nil
}

// setKDF names a retired profile only for a link whose other fields are valid, and wipes the secret on any failure.
func (l *Link) setKDF(text string) error {
	if text == KDFRetired.String() {
		l.Wipe()
		return ErrLinkRetiredKDF
	}
	kdf, err := ParseKDF(text)
	if err != nil {
		l.Wipe()
		return ErrLink
	}
	l.KDF = kdf
	return nil
}

// releasedLink recognises a well-formed link of protocol version 1 only to say that its message is gone.
func releasedLink(path, fragment string) bool {
	encodedID, ok := strings.CutPrefix(path, "/m/")
	fields := strings.Split(fragment, ".")
	if !ok || len(fields) != 3 || fields[0] != "1" || len(fields[1]) != 1 || fields[1] < "0" || fields[1] > "2" {
		return false
	}
	_, idErr := ParseID(encodedID)
	secret, secretErr := DecodeExact(fields[2], SecretLen)
	clear(secret)
	return idErr == nil && secretErr == nil
}

func decodeInto(destination []byte, text string) bool {
	decoded, err := DecodeExact(text, len(destination))
	if err != nil {
		return false
	}
	copy(destination, decoded)
	clear(decoded)
	return true
}

func newerFragment(fragment string) bool {
	label, _, _ := strings.Cut(fragment, ".")
	if !versionLabel.MatchString(label) {
		return false
	}
	version, err := strconv.Atoi(label)
	return err == nil && version > FragmentVersion
}
