package pepper

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"time"
)

type Purpose string

const (
	Read     Purpose = "read"
	Verifier Purpose = "verifier"
	Manage   Purpose = "manage"
	Lease    Purpose = "lease"
	IP       Purpose = "ip"
	PoW      Purpose = "pow"
)

const (
	SecretLen = 32
	MACLen    = 32
	BucketLen = 3

	labelID  = "nylzo/v1/pepper-id"
	infoBase = "nylzo/v1/mac/"
)

var purposes = []Purpose{Read, Verifier, Manage, Lease, IP, PoW}

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
	idh := sha256.New()
	idh.Write([]byte(labelID))
	idh.Write(secret)
	k.id = idh.Sum(nil)[0]
	for _, p := range purposes {
		derived, err := hkdf.Key(sha256.New, secret, nil, infoBase+string(p), 32)
		if err != nil {
			return nil, err
		}
		var kk [32]byte
		copy(kk[:], derived)
		k.keys[p] = kk
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
	var out [MACLen]byte
	copy(out[:], m.Sum(nil))
	return out
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

func (r *Ring) Bucket(day time.Time, addr netip.Addr, coarse bool) [BucketLen]byte {
	return r.current.Bucket(day, addr, coarse)
}

func (k *Key) Bucket(day time.Time, addr netip.Addr, coarse bool) [BucketLen]byte {
	addr = addr.Unmap()
	var family, plen byte
	var masked []byte
	if addr.Is4() {
		family = 4
		plen = 32
		if coarse {
			plen = 24
		}
		a4 := addr.As4()
		masked = mask(a4[:], int(plen))
	} else {
		family = 6
		plen = 64
		if coarse {
			plen = 48
		}
		a16 := addr.As16()
		masked = mask(a16[:], int(plen))
	}
	input := make([]byte, 0, 10+3+len(masked))
	input = append(input, day.UTC().Format("2006-01-02")...)
	input = append(input, 0, family, plen)
	input = append(input, masked...)
	m := k.MAC(IP, input)
	var out [BucketLen]byte
	copy(out[:], m[:BucketLen])
	return out
}

func mask(b []byte, bits int) []byte {
	out := append([]byte(nil), b...)
	for i := range out {
		switch {
		case bits >= 8:
			bits -= 8
		case bits > 0:
			out[i] &= byte(0xff << (8 - bits))
			bits = 0
		default:
			out[i] = 0
		}
	}
	return out
}
