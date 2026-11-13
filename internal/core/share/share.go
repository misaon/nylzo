// Package share seals the expiry share E with HPKE to a one-time hybrid post-quantum key of the client (spec section
// 5.6).
package share

import (
	"crypto/ecdh"
	"crypto/hpke"
	"crypto/mlkem"
	"errors"
	"strconv"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

const (
	Suite           = 1
	KeyLen          = mlkem.EncapsulationKeySize768 + x25519Len
	EncapsulatedLen = mlkem.CiphertextSize768 + x25519Len
	WrappedLen      = EncapsulatedLen + core.ShareLen + tagLen

	x25519Len = 32
	tagLen    = 16
	info      = "nylzo/v2/share"
)

var (
	ErrShareKey = errors.New("core: the share key is not a usable MLKEM768-X25519 key of share suite 1")
	ErrOutdated = errors.New("core: the share key is the X25519 key of a client from before share suites")
	ErrWrapped  = errors.New("core: the wrapped share does not open with this share key")
)

var suitePrefix = strconv.Itoa(Suite) + "."

// lowOrderProbe finds an X25519 half of small order: a clamped scalar is a multiple of 8, so it maps every such point
// to zero, which ECDH refuses. The scalar is public and needs no secrecy.
var lowOrderProbe = mustX25519Key([x25519Len]byte{1})

func mustX25519Key(scalar [x25519Len]byte) *ecdh.PrivateKey {
	key, err := ecdh.X25519().NewPrivateKey(scalar[:])
	if err != nil {
		panic(err)
	}
	return key
}

func kem() hpke.KEM   { return hpke.MLKEM768X25519() }
func kdf() hpke.KDF   { return hpke.HKDFSHA256() }
func aead() hpke.AEAD { return hpke.AES256GCM() }

// NewKey draws the one-time key pair a client sends with a request that returns a share.
func NewKey() hpke.PrivateKey {
	key, err := kem().GenerateKey()
	if err != nil {
		panic(err)
	}
	return key
}

// NewKeyFromSeed rebuilds a key pair from the 32-byte seed that derives both of its halves.
func NewKeyFromSeed(seed []byte) (hpke.PrivateKey, error) {
	key, err := kem().NewPrivateKey(seed)
	if err != nil {
		return nil, ErrShareKey
	}
	return key, nil
}

// FormatKey is the X-Nylzo-Share-Key value: the suite, a dot and the public key.
func FormatKey(key hpke.PublicKey) string {
	return suitePrefix + core.Encode(key.Bytes())
}

func ParseKey(text string) (hpke.PublicKey, error) {
	encoded, ok := strings.CutPrefix(text, suitePrefix)
	if !ok {
		if _, err := core.DecodeExact(text, x25519Len); err == nil {
			return nil, ErrOutdated
		}
		return nil, ErrShareKey
	}
	decoded, err := core.DecodeExact(encoded, KeyLen)
	if err != nil || !usableX25519(decoded[mlkem.EncapsulationKeySize768:]) {
		return nil, ErrShareKey
	}
	key, err := kem().NewPublicKey(decoded)
	if err != nil {
		return nil, ErrShareKey
	}
	return key, nil
}

func usableX25519(half []byte) bool {
	key, err := ecdh.X25519().NewPublicKey(half)
	if err != nil {
		return false
	}
	secret, err := lowOrderProbe.ECDH(key)
	clear(secret)
	return err == nil
}

// Wrap seals the share to the client's key with a fresh encapsulation.
func Wrap(share *[core.ShareLen]byte, clientKey hpke.PublicKey) ([WrappedLen]byte, error) {
	var wrapped [WrappedLen]byte
	if clientKey.KEM().ID() != kem().ID() {
		return wrapped, ErrShareKey
	}
	sealed, err := hpke.Seal(clientKey, kdf(), aead(), []byte(info), share[:])
	if err != nil {
		return wrapped, err
	}
	if len(sealed) != WrappedLen {
		return wrapped, ErrShareKey
	}
	copy(wrapped[:], sealed)
	return wrapped, nil
}

func Unwrap(clientKey hpke.PrivateKey, wrapped *[WrappedLen]byte) ([core.ShareLen]byte, error) {
	var share [core.ShareLen]byte
	if clientKey.KEM().ID() != kem().ID() {
		return share, ErrShareKey
	}
	opened, err := hpke.Open(clientKey, kdf(), aead(), []byte(info), wrapped[:])
	defer clear(opened)
	if err != nil || len(opened) != core.ShareLen {
		return share, ErrWrapped
	}
	copy(share[:], opened)
	return share, nil
}
