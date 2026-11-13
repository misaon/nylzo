// Package opaque implements OPAQUE-3DH (RFC 9807) with the OPRF ristretto255-SHA512 of RFC 9497, HKDF-SHA-512,
// HMAC-SHA-512 and SHA-512, the configuration of protocol v3 (spec section 2.1).
package opaque

import (
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"math"

	"github.com/gtank/ristretto255"
)

const (
	// ElementLen is Noe and Npk: a serialized ristretto255 element.
	ElementLen = 32
	// ScalarLen is Nok and Nsk: a serialized ristretto255 scalar.
	ScalarLen  = 32
	uniformLen = 64
	// hashBlockLen is the input block size of SHA-512, s_in_bytes of RFC 9380 section 5.3.1.
	hashBlockLen    = 128
	maxExpandBlocks = 255

	contextString      = "OPRFV1-\x00-ristretto255-SHA512"
	dstHashToGroup     = "HashToGroup-" + contextString
	dstDeriveKeyPair   = "DeriveKeyPair" + contextString
	labelFinalize      = "Finalize"
	maxDeriveKeyPairTo = 255
)

var (
	// ErrDeserialize is DeserializeError of RFC 9497: bytes that are not a ristretto255 element other than the identity.
	ErrDeserialize = errors.New("opaque: not a ristretto255 element other than the identity")
	// ErrInvalidInput is InvalidInputError: an input that hashes to the identity, which nobody can find.
	ErrInvalidInput = errors.New("opaque: the input hashes to the identity")
	// ErrInputLength refuses an OPRF input that Finalize cannot prefix with its two-byte length.
	ErrInputLength   = errors.New("opaque: the input is longer than 65535 bytes")
	errDeriveKeyPair = errors.New("opaque: no key pair derives from this seed")
)

// DeserializeElement decodes a canonical ristretto255 element and refuses the identity (RFC 9497 section 4.1).
func DeserializeElement(encoded []byte) (*ristretto255.Element, error) {
	if len(encoded) != ElementLen {
		return nil, ErrDeserialize
	}
	element, err := ristretto255.NewIdentityElement().SetCanonicalBytes(encoded)
	if err != nil || element.Equal(ristretto255.NewIdentityElement()) == 1 {
		return nil, ErrDeserialize
	}
	return element, nil
}

func deserializeScalar(encoded []byte) (*ristretto255.Scalar, error) {
	if len(encoded) != ScalarLen {
		return nil, ErrDeserialize
	}
	return ristretto255.NewScalar().SetCanonicalBytes(encoded)
}

// NewBlind draws a scalar other than zero (RandomScalar of RFC 9497 section 4.7).
func NewBlind() [ScalarLen]byte {
	var uniform [uniformLen]byte
	defer clear(uniform[:])
	scalar := ristretto255.NewScalar()
	defer scalar.Zero()
	for {
		rand.Read(uniform[:])
		if _, err := scalar.SetUniformBytes(uniform[:]); err == nil && scalar.Equal(ristretto255.NewScalar()) == 0 {
			break
		}
	}
	encoded := scalar.Bytes()
	defer clear(encoded)
	return [ScalarLen]byte(encoded)
}

func blindFrom(encoded *[ScalarLen]byte) (*ristretto255.Scalar, error) {
	scalar, err := deserializeScalar(encoded[:])
	if err != nil || scalar.Equal(ristretto255.NewScalar()) == 1 {
		return nil, ErrDeserialize
	}
	return scalar, nil
}

// expandMessageXMD is expand_message_xmd of RFC 9380 section 5.3.1 with SHA-512.
func expandMessageXMD(message []byte, dst string, length int) []byte {
	blocks := (length + sha512.Size - 1) / sha512.Size
	if blocks > maxExpandBlocks || len(dst) > maxExpandBlocks || length > 1<<16-1 {
		panic("opaque: expand_message_xmd out of range")
	}
	dstPrime := append([]byte(dst), lengthByte(len(dst)))
	digest := sha512.New()
	digest.Write(make([]byte, hashBlockLen))
	digest.Write(message)
	digest.Write(appendLength16(nil, length))
	digest.Write([]byte{0})
	digest.Write(dstPrime)
	first := digest.Sum(nil)
	uniform := make([]byte, 0, blocks*sha512.Size)
	previous := make([]byte, sha512.Size)
	for index := 1; index <= blocks; index++ {
		mixed := first
		if index > 1 {
			mixed = make([]byte, sha512.Size)
			subtle.XORBytes(mixed, first, previous)
		}
		digest.Reset()
		digest.Write(mixed)
		digest.Write([]byte{lengthByte(index)})
		digest.Write(dstPrime)
		previous = digest.Sum(nil)
		uniform = append(uniform, previous...)
	}
	return uniform[:length]
}

// appendLength16 is I2OSP(length, 2); callers bound every length to MaxInputLen.
func appendLength16(data []byte, length int) []byte {
	if length < 0 || length > math.MaxUint16 {
		panic("opaque: length out of range")
	}
	return binary.BigEndian.AppendUint16(data, uint16(length))
}

// lengthByte is I2OSP(length, 1).
func lengthByte(length int) byte {
	if length < 0 || length > math.MaxUint8 {
		panic("opaque: length out of range")
	}
	return byte(length)
}

// hashToGroup is hash_to_ristretto255 of RFC 9380 appendix B.
func hashToGroup(input []byte) *ristretto255.Element {
	uniform := expandMessageXMD(input, dstHashToGroup, uniformLen)
	defer clear(uniform)
	element, err := ristretto255.NewIdentityElement().SetUniformBytes(uniform)
	if err != nil {
		panic(err)
	}
	return element
}

func hashToScalar(input []byte, dst string) *ristretto255.Scalar {
	uniform := expandMessageXMD(input, dst, uniformLen)
	defer clear(uniform)
	scalar, err := ristretto255.NewScalar().SetUniformBytes(uniform)
	if err != nil {
		panic(err)
	}
	return scalar
}

// deriveKeyPair is DeriveKeyPair of RFC 9497 section 3.2.1.
func deriveKeyPair(seed []byte, info string) (*ristretto255.Scalar, *ristretto255.Element, error) {
	input := make([]byte, 0, len(seed)+2+len(info)+1)
	input = append(input, seed...)
	input = appendLength16(input, len(info))
	input = append(input, info...)
	defer clear(input)
	zero := ristretto255.NewScalar()
	for counter := 0; counter <= maxDeriveKeyPairTo; counter++ {
		private := hashToScalar(append(input, byte(counter)), dstDeriveKeyPair)
		if private.Equal(zero) == 0 {
			return private, ristretto255.NewIdentityElement().ScalarBaseMult(private), nil
		}
	}
	return nil, nil, errDeriveKeyPair
}

// blind is Blind of RFC 9497 section 3.3.1 with the given blinding scalar.
func blind(input []byte, factor *ristretto255.Scalar) ([]byte, error) {
	if len(input) > MaxInputLen {
		return nil, ErrInputLength
	}
	element := hashToGroup(input)
	if element.Equal(ristretto255.NewIdentityElement()) == 1 {
		return nil, ErrInvalidInput
	}
	return ristretto255.NewIdentityElement().ScalarMult(factor, element).Bytes(), nil
}

// blindEvaluate is BlindEvaluate of RFC 9497 section 3.3.1.
func blindEvaluate(key *ristretto255.Scalar, blinded []byte) ([]byte, error) {
	element, err := DeserializeElement(blinded)
	if err != nil {
		return nil, err
	}
	return ristretto255.NewIdentityElement().ScalarMult(key, element).Bytes(), nil
}

// finalize is Finalize of RFC 9497 section 3.3.1.
func finalize(input []byte, factor *ristretto255.Scalar, evaluated []byte) ([]byte, error) {
	if len(input) > MaxInputLen {
		return nil, ErrInputLength
	}
	element, err := DeserializeElement(evaluated)
	if err != nil {
		return nil, err
	}
	inverse := ristretto255.NewScalar().Invert(factor)
	unblinded := ristretto255.NewIdentityElement().ScalarMult(inverse, element).Bytes()
	digest := sha512.New()
	digest.Write(appendLength16(nil, len(input)))
	digest.Write(input)
	digest.Write(appendLength16(nil, len(unblinded)))
	digest.Write(unblinded)
	digest.Write([]byte(labelFinalize))
	return digest.Sum(nil), nil
}
