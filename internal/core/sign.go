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
	Value     [SignatureLen]byte
}

func PublicKey(seed *[SeedLen]byte) [PublicKeyLen]byte {
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	defer clear(privateKey)
	return [PublicKeyLen]byte(privateKey[ed25519.SeedSize:])
}

func ValidPublicKey(publicKey []byte) bool {
	point, err := new(edwards25519.Point).SetBytes(publicKey)
	if err != nil || !bytes.Equal(point.Bytes(), publicKey) {
		return false
	}
	return new(edwards25519.Point).MultByCofactor(point).Equal(edwards25519.NewIdentityPoint()) == 0
}

func RequestMessage(purpose, method, path string, challenge *[ChallengeLen]byte, body []byte) []byte {
	bodyHash := sha256.Sum256(body)
	message := make([]byte, 0, len(requestLabel)+len(purpose)+len(method)+len(path)+4+ChallengeLen+len(bodyHash))
	for _, part := range []string{requestLabel, purpose, method, path} {
		message = append(message, part...)
		message = append(message, 0)
	}
	message = append(message, challenge[:]...)
	return append(message, bodyHash[:]...)
}

func SignRequest(
	seed *[SeedLen]byte,
	purpose, method, path string,
	challenge [ChallengeLen]byte,
	body []byte,
) *Signature {
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	defer clear(privateKey)
	signature := &Signature{PublicKey: [PublicKeyLen]byte(privateKey[ed25519.SeedSize:]), Challenge: challenge}
	copy(signature.Value[:], ed25519.Sign(privateKey, RequestMessage(purpose, method, path, &challenge, body)))
	return signature
}

func ParseAuthorization(header string) (Signature, error) {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || scheme != AuthScheme {
		return Signature{}, ErrAuthorization
	}
	fields := strings.Split(value, ".")
	if len(fields) != 3 {
		return Signature{}, ErrAuthorization
	}
	var signature Signature
	targets := [][]byte{signature.PublicKey[:], signature.Challenge[:], signature.Value[:]}
	for i, target := range targets {
		if !decodeInto(target, fields[i]) {
			return Signature{}, ErrAuthorization
		}
	}
	return signature, nil
}

func ParseVerifierHeader(header string, challenge [ChallengeLen]byte) (Signature, error) {
	encodedPublicKey, encodedSignature, ok := strings.Cut(header, ".")
	if !ok {
		return Signature{}, ErrAuthorization
	}
	signature := Signature{Challenge: challenge}
	if !decodeInto(signature.PublicKey[:], encodedPublicKey) || !decodeInto(signature.Value[:], encodedSignature) {
		return Signature{}, ErrAuthorization
	}
	return signature, nil
}

func (s *Signature) Verify(purpose, method, path string, body []byte) bool {
	return ed25519.Verify(s.PublicKey[:], RequestMessage(purpose, method, path, &s.Challenge, body), s.Value[:])
}

func (s *Signature) Authorization() string {
	return AuthScheme + " " + Encode(s.PublicKey[:]) + "." + Encode(s.Challenge[:]) + "." + Encode(s.Value[:])
}

func (s *Signature) VerifierHeader() string {
	return Encode(s.PublicKey[:]) + "." + Encode(s.Value[:])
}

func decodeInto(target []byte, text string) bool {
	decoded, err := DecodeExact(text, len(target))
	if err != nil {
		return false
	}
	copy(target, decoded)
	return true
}
