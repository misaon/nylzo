package passkey

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/sha256"
	"crypto/x509"
	"slices"
)

const (
	maximumPublicKeySize = 4096
	maximumSignatureSize = 8192
)

func parsePublicKey(subjectPublicKeyInfo []byte, algorithm Algorithm) (crypto.PublicKey, error) {
	if len(subjectPublicKeyInfo) == 0 || len(subjectPublicKeyInfo) > maximumPublicKeySize {
		return nil, refused("the public key has an invalid length")
	}
	publicKey, err := x509.ParsePKIXPublicKey(subjectPublicKeyInfo)
	if err != nil {
		return nil, refused("the public key is not a valid SubjectPublicKeyInfo")
	}
	if !keyMatchesAlgorithm(publicKey, algorithm) {
		return nil, refused("the public key does not match its algorithm")
	}
	return publicKey, nil
}

func keyMatchesAlgorithm(publicKey crypto.PublicKey, algorithm Algorithm) bool {
	switch typed := publicKey.(type) {
	case *ecdsa.PublicKey:
		return algorithm == ES256 && typed.Curve == elliptic.P256()
	case ed25519.PublicKey:
		return algorithm == EdDSA
	case *mldsa.PublicKey:
		parameters, ok := mldsaParameters(algorithm)
		return ok && typed.Parameters() == parameters
	default:
		return false
	}
}

func mldsaParameters(algorithm Algorithm) (mldsa.Parameters, bool) {
	switch algorithm {
	case MLDSA44:
		return mldsa.MLDSA44(), true
	case MLDSA65:
		return mldsa.MLDSA65(), true
	case MLDSA87:
		return mldsa.MLDSA87(), true
	case ES256, EdDSA:
		return mldsa.Parameters{}, false
	default:
		return mldsa.Parameters{}, false
	}
}

func verifySignature(publicKey crypto.PublicKey, authenticatorData, clientDataJSON, signature []byte) error {
	if len(signature) == 0 || len(signature) > maximumSignatureSize {
		return refused("the signature has an invalid length")
	}
	clientDataHash := sha256.Sum256(clientDataJSON)
	message := slices.Concat(authenticatorData, clientDataHash[:])
	var valid bool
	switch typed := publicKey.(type) {
	case *ecdsa.PublicKey:
		digest := sha256.Sum256(message)
		valid = ecdsa.VerifyASN1(typed, digest[:], signature)
	case ed25519.PublicKey:
		valid = ed25519.Verify(typed, message, signature)
	case *mldsa.PublicKey:
		valid = mldsa.Verify(typed, message, signature, nil) == nil
	default:
		valid = false
	}
	if !valid {
		return refused("the signature is not valid")
	}
	return nil
}
