package pepper

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

type Purpose string

const (
	Read      Purpose = "read"
	Verifier  Purpose = "verifier"
	Manage    Purpose = "manage"
	Lease     Purpose = "lease"
	PoW       Purpose = "pow"
	IDToken   Purpose = "id"
	Challenge Purpose = "challenge"
)

const (
	SecretLen = 32
	MACLen    = 32

	labelID  = "nylzo/v1/pepper-id"
	infoBase = "nylzo/v1/mac/"
)

var purposes = []Purpose{Read, Verifier, Manage, Lease, PoW, IDToken, Challenge}

var (
	ErrSecret    = errors.New("pepper: secret must be 32 bytes in hex or base64url")
	ErrWeak      = errors.New("pepper: secret has 32 equal bytes; generate one with openssl rand -hex 32")
	ErrCollision = errors.New(
		"pepper: current and previous pepper share a pepper_id; choose a different previous pepper",
	)
)

func ParseSecret(text string) ([]byte, error) {
	secret, err := decodeSecret(strings.TrimSpace(text))
	if err != nil {
		return nil, err
	}
	if bytes.Count(secret, secret[:1]) == SecretLen {
		return nil, ErrWeak
	}
	return secret, nil
}

func decodeSecret(text string) ([]byte, error) {
	if secret, err := hex.DecodeString(text); err == nil && len(secret) == SecretLen {
		return secret, nil
	}
	if secret, err := base64.RawURLEncoding.Strict().DecodeString(text); err == nil && len(secret) == SecretLen {
		return secret, nil
	}
	return nil, ErrSecret
}

type key struct {
	id      byte
	macKeys map[Purpose][32]byte
}

func newKey(secret []byte) (*key, error) {
	if len(secret) != SecretLen {
		return nil, ErrSecret
	}
	derived := &key{
		id:      sha256.Sum256(append([]byte(labelID), secret...))[0],
		macKeys: make(map[Purpose][32]byte, len(purposes)),
	}
	for _, purpose := range purposes {
		macKey, err := hkdf.Key(sha256.New, secret, nil, infoBase+string(purpose), 32)
		if err != nil {
			return nil, err
		}
		derived.macKeys[purpose] = [32]byte(macKey)
	}
	return derived, nil
}

func (k *key) mac(purpose Purpose, input []byte) [MACLen]byte {
	macKey, ok := k.macKeys[purpose]
	if !ok {
		panic("pepper: unknown purpose " + strconv.Quote(string(purpose)))
	}
	hash := hmac.New(sha256.New, macKey[:])
	hash.Write(input)
	return [MACLen]byte(hash.Sum(nil))
}

type Ring struct {
	current  *key
	previous *key
	dummy    [MACLen]byte
}

func NewRing(current, previous []byte) (*Ring, error) {
	currentKey, err := newKey(current)
	if err != nil {
		return nil, err
	}
	ring := &Ring{current: currentKey, dummy: currentKey.mac(Read, []byte("nylzo/v1/dummy"))}
	if previous == nil {
		return ring, nil
	}
	ring.previous, err = newKey(previous)
	if err != nil {
		return nil, err
	}
	if ring.previous.id == currentKey.id {
		return nil, ErrCollision
	}
	return ring, nil
}

func (r *Ring) CurrentID() byte {
	return r.current.id
}

//nolint:gocritic // unnamedResult: internal/server calls this signature; see the cross-area list.
func (r *Ring) MAC(purpose Purpose, input []byte) ([MACLen]byte, byte) {
	return r.current.mac(purpose, input), r.current.id
}

func (r *Ring) Verify(purpose Purpose, input, expected []byte, id byte) bool {
	verifying := r.keyFor(id)
	known := verifying != nil
	if !known {
		verifying = r.current
		expected = r.dummy[:]
	}
	computed := verifying.mac(purpose, input)
	return hmac.Equal(computed[:], expected) && known
}

func (r *Ring) Dummy() []byte {
	return append([]byte(nil), r.dummy[:]...)
}

func (r *Ring) keyFor(id byte) *key {
	if r.current.id == id {
		return r.current
	}
	if r.previous != nil && r.previous.id == id {
		return r.previous
	}
	return nil
}
