package pepper

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

type Purpose string

const (
	Read      Purpose = "read"
	Verifier  Purpose = "verifier"
	Manage    Purpose = "manage"
	Lease     Purpose = "lease"
	PoW       Purpose = "pow"
	ID        Purpose = "id"
	Challenge Purpose = "challenge"
)

const (
	SecretLen = 32
	MACLen    = 32

	labelID  = "nylzo/v1/pepper-id"
	infoBase = "nylzo/v1/mac/"
)

var purposes = []Purpose{Read, Verifier, Manage, Lease, PoW, ID, Challenge}

var (
	ErrSecret    = errors.New("pepper: secret must be 32 bytes in hex or base64url")
	ErrCollision = errors.New("pepper: current and previous pepper share a pepper_id; choose a different previous pepper")
)

type Key struct {
	id   byte
	keys map[Purpose][32]byte
}

func New(secret []byte) (*Key, error) {
	if len(secret) != SecretLen {
		return nil, ErrSecret
	}
	k := &Key{keys: make(map[Purpose][32]byte, len(purposes))}
	k.id = sha256.Sum256(append([]byte(labelID), secret...))[0]
	for _, p := range purposes {
		derived, err := hkdf.Key(sha256.New, secret, nil, infoBase+string(p), 32)
		if err != nil {
			return nil, err
		}
		k.keys[p] = [32]byte(derived)
	}
	return k, nil
}

func ParseSecret(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := hex.DecodeString(s); err == nil && len(b) == SecretLen {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.Strict().DecodeString(s); err == nil && len(b) == SecretLen {
		return b, nil
	}
	return nil, ErrSecret
}

func (k *Key) ID() byte {
	return k.id
}

func (k *Key) MAC(p Purpose, input []byte) [MACLen]byte {
	key := k.keys[p]
	m := hmac.New(sha256.New, key[:])
	m.Write(input)
	return [MACLen]byte(m.Sum(nil))
}

type Ring struct {
	current  *Key
	previous *Key
	dummy    [MACLen]byte
}

func NewRing(current, previous []byte) (*Ring, error) {
	cur, err := New(current)
	if err != nil {
		return nil, err
	}
	r := &Ring{current: cur}
	if previous != nil {
		prev, err := New(previous)
		if err != nil {
			return nil, err
		}
		if prev.id == cur.id {
			return nil, ErrCollision
		}
		r.previous = prev
	}
	r.dummy = cur.MAC(Read, []byte("nylzo/v1/dummy"))
	return r, nil
}

func (r *Ring) CurrentID() byte {
	return r.current.id
}

func (r *Ring) MAC(p Purpose, input []byte) ([MACLen]byte, byte) {
	return r.current.MAC(p, input), r.current.id
}

func (r *Ring) byID(id byte) *Key {
	if r.current.id == id {
		return r.current
	}
	if r.previous != nil && r.previous.id == id {
		return r.previous
	}
	return nil
}

func (r *Ring) Verify(p Purpose, input []byte, expected []byte, id byte) bool {
	key := r.byID(id)
	known := key != nil
	if !known {
		key = r.current
		expected = r.dummy[:]
	}
	got := key.MAC(p, input)
	return hmac.Equal(got[:], expected) && known
}

func (r *Ring) Dummy() []byte {
	return append([]byte(nil), r.dummy[:]...)
}
