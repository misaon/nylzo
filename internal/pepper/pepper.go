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

	"github.com/misaon/nylzo/internal/core/opaque"
)

type Purpose string

const (
	Read         Purpose = "read"
	Manage       Purpose = "manage"
	Lease        Purpose = "lease"
	PoW          Purpose = "pow"
	IDToken      Purpose = "id"
	Challenge    Purpose = "challenge"
	OPAQUEServer Purpose = "opaque-server"
)

const (
	SecretLen = 32
	MACLen    = 32

	labelID      = "nylzo/v1/pepper-id"
	infoBase     = "nylzo/v1/mac/"
	infoRecords  = "nylzo/v1/records"
	infoOPRFSeed = "nylzo/v3/opaque/oprf-seed"
)

var purposes = []Purpose{Read, Manage, Lease, PoW, IDToken, Challenge, OPAQUEServer}

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
	id         byte
	macKeys    map[Purpose][32]byte
	recordsKey [SecretLen]byte
	oprfSeed   [opaque.OPRFSeedLen]byte
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
		macKey, err := hkdf.Key(sha256.New, secret, nil, infoBase+string(purpose), sha256.Size)
		if err != nil {
			return nil, err
		}
		derived.macKeys[purpose] = [32]byte(macKey)
	}
	recordsKey, err := hkdf.Key(sha256.New, secret, nil, infoRecords, SecretLen)
	if err != nil {
		return nil, err
	}
	derived.recordsKey = [SecretLen]byte(recordsKey)
	clear(recordsKey)
	oprfSeed, err := hkdf.Key(sha256.New, secret, nil, infoOPRFSeed, opaque.OPRFSeedLen)
	if err != nil {
		return nil, err
	}
	derived.oprfSeed = [opaque.OPRFSeedLen]byte(oprfSeed)
	clear(oprfSeed)
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

// RecordsKey seals the record key wraps (spec §5.2).
func (r *Ring) RecordsKey() [SecretLen]byte {
	return r.current.recordsKey
}

// PreviousRecordsKey is the records key of the previous pepper, until ForgetPreviousRecordsKey erases it.
func (r *Ring) PreviousRecordsKey() ([SecretLen]byte, bool) {
	if r.previous == nil || r.previous.recordsKey == [SecretLen]byte{} {
		return [SecretLen]byte{}, false
	}
	return r.previous.recordsKey, true
}

// ForgetPreviousRecordsKey erases the records key of the previous pepper once serve has sealed every row again; the
// macs of the previous pepper still verify.
func (r *Ring) ForgetPreviousRecordsKey() {
	if r.previous != nil {
		clear(r.previous.recordsKey[:])
	}
}

func (r *Ring) CurrentID() byte {
	return r.current.id
}

type Tag struct {
	MAC      [MACLen]byte
	PepperID byte
}

func (r *Ring) MAC(purpose Purpose, input []byte) Tag {
	return Tag{MAC: r.current.mac(purpose, input), PepperID: r.current.id}
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

// OPAQUEKeys derives the OPAQUE keys of one message from the pepper that pepperID names (spec section 5.1). With an
// unknown pepperID it derives them from the current pepper and reports false, so the work is the same.
func (r *Ring) OPAQUEKeys(messageID []byte, pepperID byte) (opaque.ServerKeys, bool, error) {
	deriving := r.keyFor(pepperID)
	known := deriving != nil
	if !known {
		deriving = r.current
	}
	seed := deriving.mac(OPAQUEServer, messageID)
	defer clear(seed[:])
	keys, err := opaque.DeriveServerKeys(seed[:], &deriving.oprfSeed)
	return keys, known, err
}

// Holds reports whether the ring still has the pepper that id names.
func (r *Ring) Holds(id byte) bool {
	return r.keyFor(id) != nil
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
