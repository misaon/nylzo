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
	idTokenLength    = 4 + idTokenTagLength
)

type IDTokens struct {
	ring *pepper.Ring
}

func NewIDTokens(ring *pepper.Ring) IDTokens {
	return IDTokens{ring: ring}
}

func (t IDTokens) Issue(id core.ID, now time.Time) (string, time.Time) {
	expires := now.Add(idTokenTTL)
	token := make([]byte, idTokenLength)
	binary.BigEndian.PutUint32(token, unixSeconds(expires))
	copy(token[4:], t.tag(id, token[:4]))
	return core.Encode(token), expires.Truncate(time.Second)
}

func (t IDTokens) Valid(id core.ID, token string, now time.Time) bool {
	raw, err := core.DecodeExact(token, idTokenLength)
	if err != nil {
		return false
	}
	tagOK := hmac.Equal(t.tag(id, raw[:4]), raw[4:])
	left := int64(binary.BigEndian.Uint32(raw[:4])) - now.Unix()
	return tagOK && left >= 0 && left <= int64((idTokenTTL+clockSkew)/time.Second)
}

func (t IDTokens) tag(id core.ID, expires []byte) []byte {
	mac := t.ring.MAC(pepper.IDToken, append(id[:], expires...)).MAC
	return mac[:idTokenTagLength]
}
