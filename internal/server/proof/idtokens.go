package proof

import (
	"crypto/hmac"
	"encoding/binary"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	idTokenTTL       = 10 * time.Minute
	idTokenTagLength = 16
	RatchetIDLen     = 16
	idTokenFieldsLen = 8 + RatchetIDLen
	idTokenLength    = idTokenFieldsLen + idTokenTagLength
)

type IDTokens struct {
	ring *pepper.Ring
}

func NewIDTokens(ring *pepper.Ring) IDTokens {
	return IDTokens{ring: ring}
}

// IDTokenTTL is how long a client may take from the instance response to the start of its upload.
func IDTokenTTL() time.Duration {
	return idTokenTTL + clockSkew
}

// Issue binds the id to the epoch of its expiry share and to the ratchet whose seed made it (spec section 5.4).
func (t IDTokens) Issue(id core.ID, epoch uint32, ratchetID [RatchetIDLen]byte, now time.Time) (string, time.Time) {
	expires := now.Add(idTokenTTL)
	token := make([]byte, idTokenLength)
	binary.BigEndian.PutUint32(token, unixSeconds(expires))
	binary.BigEndian.PutUint32(token[4:], epoch)
	copy(token[8:idTokenFieldsLen], ratchetID[:])
	copy(token[idTokenFieldsLen:], t.tag(id, token[:idTokenFieldsLen]))
	return core.Encode(token), expires.Truncate(time.Second)
}

// Valid also returns the ratchet the token names, so a create can tell a share of an earlier seed apart.
func (t IDTokens) Valid(id core.ID, epoch uint32, token string, now time.Time) ([RatchetIDLen]byte, bool) {
	var ratchetID [RatchetIDLen]byte
	raw, err := core.DecodeExact(token, idTokenLength)
	if err != nil {
		return ratchetID, false
	}
	fields := binary.BigEndian.AppendUint32(append([]byte(nil), raw[:4]...), epoch)
	fields = append(fields, raw[8:idTokenFieldsLen]...)
	tagOK := hmac.Equal(t.tag(id, fields), raw[idTokenFieldsLen:])
	epochOK := binary.BigEndian.Uint32(raw[4:8]) == epoch
	left := int64(binary.BigEndian.Uint32(raw[:4])) - now.Unix()
	copy(ratchetID[:], raw[8:idTokenFieldsLen])
	return ratchetID, tagOK && epochOK && left >= 0 && left <= int64((idTokenTTL+clockSkew)/time.Second)
}

func (t IDTokens) tag(id core.ID, fields []byte) []byte {
	mac := t.ring.MAC(pepper.IDToken, append(id[:], fields...)).MAC
	return mac[:idTokenTagLength]
}
