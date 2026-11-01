// Package share wraps the expiry share E to a one-time X25519 key of the client (spec section 5.6).
package share

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"

	"github.com/misaon/nylzo/internal/core"
)

const (
	KeyLen     = 32
	WrappedLen = KeyLen + core.ShareLen

	info = "nylzo/v2/share"
)

var ErrShareKey = errors.New("core: the share key is not a usable X25519 public key")

// lowOrderProbe finds a key of small order: a clamped scalar is a multiple of 8, so it maps every such point to
// zero, which ECDH refuses. The scalar is public and needs no secrecy.
var lowOrderProbe = mustPrivateKey([KeyLen]byte{1})

func mustPrivateKey(scalar [KeyLen]byte) *ecdh.PrivateKey {
	key, err := ecdh.X25519().NewPrivateKey(scalar[:])
	if err != nil {
		panic(err)
	}
	return key
}

// NewKey draws the one-time key pair a client sends with a request that returns a share.
func NewKey() *ecdh.PrivateKey {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return key
}

func ParseKey(text string) (*ecdh.PublicKey, error) {
	decoded, err := core.DecodeExact(text, KeyLen)
	if err != nil {
		return nil, ErrShareKey
	}
	key, err := ecdh.X25519().NewPublicKey(decoded)
	if err != nil {
		return nil, ErrShareKey
	}
	secret, err := lowOrderProbe.ECDH(key)
	clear(secret)
	if err != nil {
		return nil, ErrShareKey
	}
	return key, nil
}

// Wrap seals the share to the client's key with a fresh server key pair.
func Wrap(share *[core.ShareLen]byte, clientKey *ecdh.PublicKey) ([WrappedLen]byte, error) {
	return WrapWith(NewKey(), share, clientKey)
}

func WrapWith(
	serverKey *ecdh.PrivateKey,
	share *[core.ShareLen]byte,
	clientKey *ecdh.PublicKey,
) ([WrappedLen]byte, error) {
	var wrapped [WrappedLen]byte
	serverPublic := serverKey.PublicKey().Bytes()
	mask, err := maskOf(serverKey, clientKey, clientKey.Bytes(), serverPublic)
	if err != nil {
		return wrapped, err
	}
	defer clear(mask)
	copy(wrapped[:KeyLen], serverPublic)
	for i := range core.ShareLen {
		wrapped[KeyLen+i] = share[i] ^ mask[i]
	}
	return wrapped, nil
}

func Unwrap(clientKey *ecdh.PrivateKey, wrapped *[WrappedLen]byte) ([core.ShareLen]byte, error) {
	var share [core.ShareLen]byte
	serverKey, err := ecdh.X25519().NewPublicKey(wrapped[:KeyLen])
	if err != nil {
		return share, ErrShareKey
	}
	mask, err := maskOf(clientKey, serverKey, clientKey.PublicKey().Bytes(), wrapped[:KeyLen])
	if err != nil {
		return share, err
	}
	defer clear(mask)
	for i := range core.ShareLen {
		share[i] = wrapped[KeyLen+i] ^ mask[i]
	}
	return share, nil
}

func maskOf(private *ecdh.PrivateKey, public *ecdh.PublicKey, clientPublic, serverPublic []byte) ([]byte, error) {
	secret, err := private.ECDH(public)
	if err != nil {
		return nil, ErrShareKey
	}
	defer clear(secret)
	salt := make([]byte, 0, 2*KeyLen)
	salt = append(append(salt, clientPublic...), serverPublic...)
	return hkdf.Key(sha256.New, secret, salt, info, core.ShareLen)
}
