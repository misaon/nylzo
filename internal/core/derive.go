package core

import (
	"crypto/hkdf"
	"crypto/sha256"
)

const (
	labelHSalt    = "nylzo/v1/hkdf-salt"
	infoRead      = "nylzo/v1/read"
	infoPWSalt    = "nylzo/v1/pw-salt"
	infoContent   = "nylzo/v1/content"
	infoVerifier  = "nylzo/v1/verifier"
	FragmentLabel = string(rune('0' + FragmentVersion))
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

func DeriveLink(id ID, s *[SecretLen]byte) (LinkKeys, error) {
	var out LinkKeys
	hs := HSalt(id)
	prk, err := hkdf.Extract(sha256.New, s[:], hs[:])
	if err != nil {
		return out, err
	}
	defer clear(prk)
	r, err := hkdf.Expand(sha256.New, prk, infoRead, TokenLen)
	if err != nil {
		return out, err
	}
	defer clear(r)
	salt, err := hkdf.Expand(sha256.New, prk, infoPWSalt, 32)
	if err != nil {
		return out, err
	}
	defer clear(salt)
	copy(out.R[:], r)
	copy(out.SaltPW[:], salt)
	return out, nil
}

func DeriveContent(id ID, s *[SecretLen]byte, p []byte) (ContentKeys, error) {
	var out ContentKeys
	hs := HSalt(id)
	ikm := make([]byte, 0, SecretLen+len(p))
	ikm = append(ikm, s[:]...)
	ikm = append(ikm, p...)
	defer clear(ikm)
	prk, err := hkdf.Extract(sha256.New, ikm, hs[:])
	if err != nil {
		return out, err
	}
	defer clear(prk)
	k, err := hkdf.Expand(sha256.New, prk, infoContent, KeyLen)
	if err != nil {
		return out, err
	}
	defer clear(k)
	v, err := hkdf.Expand(sha256.New, prk, infoVerifier, TokenLen)
	if err != nil {
		return out, err
	}
	defer clear(v)
	copy(out.K[:], k)
	copy(out.V[:], v)
	return out, nil
}

func DeriveAll(id ID, s *[SecretLen]byte, kdf KDF, passphrase string) (LinkKeys, ContentKeys, error) {
	if err := checkPassphrase(kdf, passphrase); err != nil {
		return LinkKeys{}, ContentKeys{}, err
	}
	link, err := DeriveLink(id, s)
	if err != nil {
		return link, ContentKeys{}, err
	}
	p, err := DerivePassphraseKey(kdf, passphrase, &link.SaltPW)
	if err != nil {
		return link, ContentKeys{}, err
	}
	content, err := DeriveContent(id, s, p)
	return link, content, err
}
