package core

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"strings"

	"filippo.io/edwards25519"
)

const (
	PublicKeyLen = ed25519.PublicKeySize
	SignatureLen = ed25519.SignatureSize
	ChallengeLen = 36

	PurposeRead     = "read"
	PurposeVerifier = "verifier"
	PurposeManage   = "manage"

	AuthScheme   = "Nylzo-Sig"
	requestLabel = "nylzo/v1/request"
)

var ErrAuthorization = errors.New("core: malformed request signature")

type Signature struct {
	PublicKey [PublicKeyLen]byte
	Challenge [ChallengeLen]byte
	Sig       [SignatureLen]byte
}

func PublicKey(seed *[TokenLen]byte) [PublicKeyLen]byte {
	key := ed25519.NewKeyFromSeed(seed[:])
	defer clear(key)
	return [PublicKeyLen]byte(key.Public().(ed25519.PublicKey))
}

func ValidPublicKey(pk []byte) bool {
	p, err := new(edwards25519.Point).SetBytes(pk)
	if err != nil || !bytes.Equal(p.Bytes(), pk) {
		return false
	}
	return new(edwards25519.Point).MultByCofactor(p).Equal(edwards25519.NewIdentityPoint()) == 0
}

func RequestMessage(purpose, method, path string, challenge *[ChallengeLen]byte, body []byte) []byte {
	bodyHash := sha256.Sum256(body)
	msg := make([]byte, 0, len(requestLabel)+len(purpose)+len(method)+len(path)+4+ChallengeLen+len(bodyHash))
	for _, part := range []string{requestLabel, purpose, method, path} {
		msg = append(msg, part...)
		msg = append(msg, 0)
	}
	msg = append(msg, challenge[:]...)
	return append(msg, bodyHash[:]...)
}

func SignRequest(seed *[TokenLen]byte, purpose, method, path string, challenge [ChallengeLen]byte, body []byte) Signature {
	key := ed25519.NewKeyFromSeed(seed[:])
	defer clear(key)
	out := Signature{PublicKey: PublicKey(seed), Challenge: challenge}
	copy(out.Sig[:], ed25519.Sign(key, RequestMessage(purpose, method, path, &challenge, body)))
	return out
}

func (s Signature) Verify(purpose, method, path string, body []byte) bool {
	return ed25519.Verify(s.PublicKey[:], RequestMessage(purpose, method, path, &s.Challenge, body), s.Sig[:])
}

func (s Signature) Authorization() string {
	return AuthScheme + " " + Encode(s.PublicKey[:]) + "." + Encode(s.Challenge[:]) + "." + Encode(s.Sig[:])
}

func (s Signature) VerifierHeader() string {
	return Encode(s.PublicKey[:]) + "." + Encode(s.Sig[:])
}

func ParseAuthorization(header string) (Signature, error) {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || scheme != AuthScheme {
		return Signature{}, ErrAuthorization
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return Signature{}, ErrAuthorization
	}
	var out Signature
	for _, f := range []struct {
		dst  []byte
		text string
	}{{out.PublicKey[:], parts[0]}, {out.Challenge[:], parts[1]}, {out.Sig[:], parts[2]}} {
		raw, err := DecodeExact(f.text, len(f.dst))
		if err != nil {
			return Signature{}, ErrAuthorization
		}
		copy(f.dst, raw)
	}
	return out, nil
}

func ParseVerifierHeader(header string, challenge [ChallengeLen]byte) (Signature, error) {
	pk, sig, ok := strings.Cut(header, ".")
	if !ok {
		return Signature{}, ErrAuthorization
	}
	out := Signature{Challenge: challenge}
	rawPK, err := DecodeExact(pk, PublicKeyLen)
	if err != nil {
		return Signature{}, ErrAuthorization
	}
	rawSig, err := DecodeExact(sig, SignatureLen)
	if err != nil {
		return Signature{}, ErrAuthorization
	}
	copy(out.PublicKey[:], rawPK)
	copy(out.Sig[:], rawSig)
	return out, nil
}
