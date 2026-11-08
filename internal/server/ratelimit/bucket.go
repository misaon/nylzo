package ratelimit

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"net/netip"
	"sync"
)

const bucketLength = 3

const (
	familyIPv4 byte = 4
	familyIPv6 byte = 6

	sourcePrefixIPv6, sourcePrefixIPv4 byte = 64, 32
	rangePrefixIPv6, rangePrefixIPv4   byte = 48, 24
	widePrefixIPv6, widePrefixIPv4     byte = 32, 16
)

type Bucket [bucketLength]byte

// AddressKey is k_addr of spec §5.3; Rotate draws a new one, so a memory image cannot test an address seen earlier.
type AddressKey struct {
	mutex  sync.RWMutex
	secret [sha256.Size]byte
}

func NewAddressKey() *AddressKey {
	key := &AddressKey{}
	rand.Read(key.secret[:])
	return key
}

func (k *AddressKey) Rotate() {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	rand.Read(k.secret[:])
}

func (k *AddressKey) Source(address netip.Addr) Bucket {
	return k.bucket(address, sourcePrefixIPv6, sourcePrefixIPv4)
}

func (k *AddressKey) Range(address netip.Addr) Bucket {
	return k.bucket(address, rangePrefixIPv6, rangePrefixIPv4)
}

// Wide is the /32 of IPv6 or the /16 of IPv4, which bounds what one provider or allocation may take of a global cap.
func (k *AddressKey) Wide(address netip.Addr) Bucket {
	return k.bucket(address, widePrefixIPv6, widePrefixIPv4)
}

func (k *AddressKey) bucket(address netip.Addr, ipv6Bits, ipv4Bits byte) Bucket {
	address = address.Unmap()
	family, bits := familyIPv6, ipv6Bits
	if address.Is4() {
		family, bits = familyIPv4, ipv4Bits
	}
	prefix, _ := address.Prefix(int(bits))
	k.mutex.RLock()
	mac := hmac.New(sha256.New, k.secret[:])
	k.mutex.RUnlock()
	mac.Write([]byte{family, bits})
	mac.Write(prefix.Addr().AsSlice())
	return Bucket(mac.Sum(nil)[:bucketLength])
}
