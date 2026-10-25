package core

import (
	"crypto/hkdf"
	"crypto/sha256"
)

const (
	labelHSalt   = "nylzo/v1/hkdf-salt"
	infoRead     = "nylzo/v1/read"
	infoKDFSalt  = "nylzo/v1/pw-salt"
	infoContent  = "nylzo/v1/content"
	infoVerifier = "nylzo/v1/verifier"
)

type LinkKeys struct {
	R      [TokenLen]byte
	SaltPW [32]byte
}

type ContentKeys struct {
	K [KeyLen]byte
	V [TokenLen]byte
}

func HSalt(id ID) [32]byte {
	return sha256.Sum256(append([]byte(labelHSalt), id[:]...))
}

func DeriveLink(id ID, secret *[SecretLen]byte) (LinkKeys, error) {
	var keys LinkKeys
	err := expandAll(id, secret[:], []expansion{{infoRead, keys.R[:]}, {infoKDFSalt, keys.SaltPW[:]}})
	if err != nil {
		return LinkKeys{}, err
	}
	return keys, nil
}

func DeriveContent(id ID, secret *[SecretLen]byte, passphraseKey []byte) (ContentKeys, error) {
	keyMaterial := make([]byte, 0, SecretLen+len(passphraseKey))
	keyMaterial = append(keyMaterial, secret[:]...)
	keyMaterial = append(keyMaterial, passphraseKey...)
	defer clear(keyMaterial)
	var keys ContentKeys
	err := expandAll(id, keyMaterial, []expansion{{infoContent, keys.K[:]}, {infoVerifier, keys.V[:]}})
	if err != nil {
		return ContentKeys{}, err
	}
	return keys, nil
}

func DeriveAll(id ID, secret *[SecretLen]byte, kdf KDF, passphrase string) (LinkKeys, ContentKeys, error) {
	if err := checkPassphrase(kdf, passphrase); err != nil {
		return LinkKeys{}, ContentKeys{}, err
	}
	link, err := DeriveLink(id, secret)
	if err != nil {
		return link, ContentKeys{}, err
	}
	passphraseKey, err := DerivePassphraseKey(kdf, passphrase, &link.SaltPW)
	if err != nil {
		return link, ContentKeys{}, err
	}
	content, err := DeriveContent(id, secret, passphraseKey)
	return link, content, err
}

type expansion struct {
	info   string
	output []byte
}

func expandAll(id ID, keyMaterial []byte, expansions []expansion) error {
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
