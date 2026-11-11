package signing

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"

	"filippo.io/edwards25519"

	"github.com/misaon/nylzo/internal/core"
)

const (
	PublicKeyLen = ed25519.PublicKeySize
	SignatureLen = ed25519.SignatureSize
	ChallengeLen = 36

	PurposeRead     = "read"
	PurposeVerifier = "verifier"
	PurposeManage   = "manage"

	AuthScheme   = "Nylzo-Sig"
	requestLabel = "nylzo/v2/request"
)

var ErrAuthorization = errors.New("core: malformed request signature")

type Signature struct {
	PublicKey [PublicKeyLen]byte
	Challenge [ChallengeLen]byte
	Value     [SignatureLen]byte
}

func PublicKey(seed *[core.SeedLen]byte) [PublicKeyLen]byte {
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

// Components contains the security-relevant request headers, as sent on the wire.
// Missing headers are represented by the empty string. Header order and casing do not matter.
type Components struct {
	ShareKey string `json:"share_key"`
	Lease    string `json:"lease"`
}

func RequestMessage(
	purpose, method, path string,
	challenge *[ChallengeLen]byte,
	body []byte,
	components Components,
) []byte {
	bodyHash := sha256.Sum256(body)
	message := make([]byte, 0, len(requestLabel)+len(purpose)+len(method)+len(path)+4+ChallengeLen+len(bodyHash))
	for _, part := range []string{requestLabel, purpose, method, path} {
		message = append(message, part...)
		message = append(message, 0)
	}
	message = append(message, challenge[:]...)
	message = append(message, bodyHash[:]...)
	// Fixed order and length prefixes bind both values without delimiter ambiguities.
	for _, value := range []string{components.ShareKey, components.Lease} {
		message = binary.BigEndian.AppendUint64(message, uint64(len(value)))
		message = append(message, value...)
	}
	return message
}

func SignRequest(
	seed *[core.SeedLen]byte,
	purpose, method, path string,
	challenge [ChallengeLen]byte,
	body []byte,
	components Components,
) *Signature {
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	defer clear(privateKey)
	signature := &Signature{PublicKey: [PublicKeyLen]byte(privateKey[ed25519.SeedSize:]), Challenge: challenge}
	copy(
		signature.Value[:],
		ed25519.Sign(privateKey, RequestMessage(purpose, method, path, &challenge, body, components)),
	)
	return signature
}

func ParseAuthorization(header string) (Signature, error) {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || scheme != AuthScheme {
		return Signature{}, ErrAuthorization
	}
	var signature Signature
	targets := [][]byte{signature.PublicKey[:], signature.Challenge[:], signature.Value[:]}
	fields := strings.Split(value, ".")
	if len(fields) != len(targets) {
		return Signature{}, ErrAuthorization
	}
	for i, target := range targets {
		if !core.DecodeInto(target, fields[i]) {
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
	if !core.DecodeInto(signature.PublicKey[:], encodedPublicKey) ||
		!core.DecodeInto(signature.Value[:], encodedSignature) {
		return Signature{}, ErrAuthorization
	}
	return signature, nil
}

func (s *Signature) Verify(purpose, method, path string, body []byte, components Components) bool {
	return ed25519.Verify(
		s.PublicKey[:],
		RequestMessage(purpose, method, path, &s.Challenge, body, components),
		s.Value[:],
	)
}

func (s *Signature) Authorization() string {
	return AuthScheme + " " + core.Encode(
		s.PublicKey[:],
	) + "." + core.Encode(
		s.Challenge[:],
	) + "." + core.Encode(
		s.Value[:],
	)
}

func (s *Signature) VerifierHeader() string {
	return core.Encode(s.PublicKey[:]) + "." + core.Encode(s.Value[:])
}
