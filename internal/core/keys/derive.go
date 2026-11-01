package keys

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"

	"github.com/misaon/nylzo/internal/core"
)

const (
	labelHSalt      = "nylzo/v2/hkdf-salt"
	labelRead       = "nylzo/v2/read"
	labelKDFSalt    = "nylzo/v2/pw-salt"
	labelContent    = "nylzo/v2/content"
	labelVerifier   = "nylzo/v2/verifier"
	labelCommitment = "nylzo/v2/commitment"
)

var ErrShare = errors.New("core: the content key needs the expiry share")

type LinkKeys struct {
	R              [core.SeedLen]byte
	PassphraseSalt [32]byte
}

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

func DeriveLink(id core.ID, secret *[core.SecretLen]byte) (LinkKeys, error) {
	var keys LinkKeys
	err := expandAll(id, secret[:], []expansion{
		{labelRead, keys.R[:]},
		{labelKDFSalt, keys.PassphraseSalt[:]},
	})
	return keys, err
}

// DeriveVerifier needs no share, because a reader signs its claim with V before the server sends the share.
func DeriveVerifier(id core.ID, secret *[core.SecretLen]byte, passphraseKey []byte) ([core.SeedLen]byte, error) {
	keyMaterial := concatenate(secret[:], passphraseKey)
	defer clear(keyMaterial)
	var verifier [core.SeedLen]byte
	err := expandAll(id, keyMaterial, []expansion{{labelVerifier, verifier[:]}})
	return verifier, err
}

// DeriveContent derives K and K_c from S, P and the share.
func DeriveContent(
	id core.ID,
	secret *[core.SecretLen]byte,
	passphraseKey []byte,
	share *[core.ShareLen]byte,
) (ContentKeys, error) {
	if share == nil {
		return ContentKeys{}, ErrShare
	}
	keyMaterial := concatenate(secret[:], passphraseKey, share[:])
	defer clear(keyMaterial)
	var keys ContentKeys
	err := expandAll(id, keyMaterial, []expansion{
		{labelContent, keys.K[:]},
		{labelCommitment, keys.Commitment[:]},
	})
	return keys, err
}

// Derived is everything a reader or a sender derives from a link, a passphrase and a share.
type Derived struct {
	Link     LinkKeys
	Verifier [core.SeedLen]byte
	Content  ContentKeys
}

func (d *Derived) Wipe() {
	clear(d.Link.R[:])
	clear(d.Link.PassphraseSalt[:])
	clear(d.Verifier[:])
	d.Content.Wipe()
}

func DeriveAll(
	id core.ID,
	secret *[core.SecretLen]byte,
	kdf core.KDF,
	passphrase string,
	share *[core.ShareLen]byte,
) (Derived, error) {
	var derived Derived
	if err := checkPassphrase(kdf, passphrase); err != nil {
		return derived, err
	}
	link, err := DeriveLink(id, secret)
	if err != nil {
		return derived, err
	}
	derived.Link = link
	passphraseKey, err := DerivePassphraseKey(kdf, passphrase, &link.PassphraseSalt)
	defer clear(passphraseKey)
	if err == nil {
		derived.Verifier, err = DeriveVerifier(id, secret, passphraseKey)
	}
	if err == nil {
		derived.Content, err = DeriveContent(id, secret, passphraseKey, share)
	}
	if err != nil {
		derived.Wipe()
		return Derived{}, err
	}
	return derived, nil
}

type expansion struct {
	info   string
	output []byte
}

func expandAll(id core.ID, keyMaterial []byte, expansions []expansion) error {
	hsalt := HSalt(id)
	prk, err := hkdf.Extract(sha256.New, keyMaterial, hsalt[:])
	if err != nil {
		return err
	}
	defer clear(prk)
	for _, step := range expansions {
		if err = expand(prk, step); err != nil {
			for _, filled := range expansions {
				clear(filled.output)
			}
			return err
		}
	}
	return nil
}

func expand(prk []byte, step expansion) error {
	derived, err := hkdf.Expand(sha256.New, prk, step.info, len(step.output))
	if err != nil {
		return err
	}
	copy(step.output, derived)
	clear(derived)
	return nil
}

func concatenate(parts ...[]byte) []byte {
	length := 0
	for _, part := range parts {
		length += len(part)
	}
	joined := make([]byte, 0, length)
	for _, part := range parts {
		joined = append(joined, part...)
	}
	return joined
}
