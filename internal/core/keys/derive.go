package keys

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"slices"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/opaque"
)

const (
	labelHSalt      = "nylzo/v3/hkdf-salt"
	labelRead       = "nylzo/v3/read"
	labelKDFSalt    = "nylzo/v3/pw-salt"
	labelContent    = "nylzo/v3/content"
	labelCommitment = "nylzo/v3/commitment"
)

const PassphraseSaltLen = 32

var ErrShare = errors.New("core: the content key needs the expiry share")

// ExportKey is P, the export_key of the OPAQUE registration or handshake (spec section 2.1).
type ExportKey = [opaque.ExportKeyLen]byte

// ContentKeys holds K and the commitment key K_c.
type ContentKeys struct {
	K          [core.KeyLen]byte
	Commitment [core.KeyLen]byte
}

func (c *ContentKeys) Wipe() {
	clear(c.K[:])
	clear(c.Commitment[:])
}

func HSalt(id core.ID) [32]byte {
	return sha256.Sum256(append([]byte(labelHSalt), id[:]...))
}

// DeriveRead derives R, the seed of the read key.
func DeriveRead(id core.ID, secret *[core.SecretLen]byte) [core.SeedLen]byte {
	var read [core.SeedLen]byte
	expandAll(id, secret[:], []expansion{{labelRead, read[:]}})
	return read
}

// PassphraseSalt derives salt_pw from S alone: a sender needs it before the server has issued the id.
func PassphraseSalt(secret *[core.SecretLen]byte) [PassphraseSaltLen]byte {
	var salt [PassphraseSaltLen]byte
	derived, _ := hkdf.Key(sha256.New, secret[:], nil, labelKDFSalt, len(salt))
	copy(salt[:], derived)
	clear(derived)
	return salt
}

// CredentialIdentifier is the credential_identifier of OPAQUE for a message: its id and its expiry share, so the OPRF
// key that tests a passphrase stops existing with the share (spec section 5.1).
func CredentialIdentifier(id core.ID, share *[core.ShareLen]byte) []byte {
	return slices.Concat(id[:], share[:])
}

func DeriveContent(
	id core.ID,
	secret *[core.SecretLen]byte,
	exportKey *ExportKey,
	share *[core.ShareLen]byte,
) (ContentKeys, error) {
	if share == nil {
		return ContentKeys{}, ErrShare
	}
	keyMaterial := slices.Concat(secret[:], exportKey[:], share[:])
	defer clear(keyMaterial)
	var keys ContentKeys
	expandAll(id, keyMaterial, []expansion{
		{labelContent, keys.K[:]},
		{labelCommitment, keys.Commitment[:]},
	})
	return keys, nil
}

type expansion struct {
	info   string
	output []byte
}

func expandAll(id core.ID, keyMaterial []byte, expansions []expansion) {
	hsalt := HSalt(id)
	prk, _ := hkdf.Extract(sha256.New, keyMaterial, hsalt[:])
	defer clear(prk)
	for _, step := range expansions {
		derived, _ := hkdf.Expand(sha256.New, prk, step.info, len(step.output))
		copy(step.output, derived)
		clear(derived)
	}
}
