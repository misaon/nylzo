package core

import (
	"crypto/rand"
	"crypto/sha256"
)

type ID [IDLen]byte

func NewID() ID {
	var id ID
	rand.Read(id[:])
	return id
}

func ParseID(s string) (ID, error) {
	var id ID
	b, err := DecodeExact(s, IDLen)
	if err != nil {
		return id, err
	}
	copy(id[:], b)
	return id, nil
}

func (id ID) String() string {
	return Encode(id[:])
}

func NewSecret() *[SecretLen]byte {
	var s [SecretLen]byte
	rand.Read(s[:])
	return &s
}

func NewIV() [IVLen]byte {
	var iv [IVLen]byte
	rand.Read(iv[:])
	return iv
}

func Hash(b []byte) [32]byte {
	return sha256.Sum256(b)
}
