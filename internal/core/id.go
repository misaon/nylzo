package core

import "crypto/rand"

const (
	IDLen         = 16
	SecretLen     = 32
	KeyLen        = 32
	SeedLen       = 32
	ObjectHashLen = 32
	ShareLen      = 32
)

type ID [IDLen]byte

func NewID() ID {
	var id ID
	rand.Read(id[:])
	return id
}

func ParseID(text string) (ID, error) {
	decoded, err := DecodeExact(text, IDLen)
	if err != nil {
		return ID{}, err
	}
	return ID(decoded), nil
}

func (id ID) String() string {
	return Encode(id[:])
}

func NewSecret() *[SecretLen]byte {
	var secret [SecretLen]byte
	rand.Read(secret[:])
	return &secret
}
