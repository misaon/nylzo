package ratelimit

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"net/netip"
)

const bucketLength = 3

type Bucket [bucketLength]byte

type AddressKey struct {
	secret [sha256.Size]byte
}

func NewAddressKey() *AddressKey {
	key := &AddressKey{}
	rand.Read(key.secret[:])
	return key
}

func (k *AddressKey) Source(address netip.Addr) Bucket {
	return k.bucket(address, 64, 32)
}

func (k *AddressKey) Range(address netip.Addr) Bucket {
	return k.bucket(address, 48, 24)
}

func (k *AddressKey) bucket(address netip.Addr, ipv6Bits, ipv4Bits byte) Bucket {
	address = address.Unmap()
	family, bits := byte(6), ipv6Bits
	if address.Is4() {
		family, bits = 4, ipv4Bits
	}
	prefix, _ := address.Prefix(int(bits))
	mac := hmac.New(sha256.New, k.secret[:])
	mac.Write([]byte{family, bits})
	mac.Write(prefix.Addr().AsSlice())
	return Bucket(mac.Sum(nil)[:bucketLength])
}
