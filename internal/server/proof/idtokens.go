package proof

import (
	"bytes"
	"crypto/hmac"
	"encoding/binary"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	idTokenTTL       = 10 * time.Minute
	idTokenTagLength = 16
	idTokenFieldsLen = 8 + epoch.GenerationIDLen
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

// Issue binds the id to the epoch of its expiry share and to the generation whose seed made it (spec section 5.4).
func (t IDTokens) Issue(
	id core.ID,
	shareEpoch uint32,
	generation epoch.GenerationID,
	now time.Time,
) (string, time.Time) {
	expires := now.Add(idTokenTTL)
	token := make([]byte, idTokenLength)
	binary.BigEndian.PutUint32(token, unixSeconds(expires))
	binary.BigEndian.PutUint32(token[4:], shareEpoch)
	copy(token[8:idTokenFieldsLen], generation[:])
	copy(token[idTokenFieldsLen:], t.tag(id, token[:idTokenFieldsLen]))
	return core.Encode(token), expires.Truncate(time.Second)
}

// Valid also returns the generation the token names, so a create can tell a share of a lost seed apart.
func (t IDTokens) Valid(id core.ID, shareEpoch uint32, token string, now time.Time) (epoch.GenerationID, bool) {
	var generation epoch.GenerationID
	raw, err := core.DecodeExact(token, idTokenLength)
	if err != nil {
		return generation, false
	}
	fields := binary.BigEndian.AppendUint32(bytes.Clone(raw[:4]), shareEpoch)
	fields = append(fields, raw[8:idTokenFieldsLen]...)
	tagOK := hmac.Equal(t.tag(id, fields), raw[idTokenFieldsLen:])
	epochOK := binary.BigEndian.Uint32(raw[4:8]) == shareEpoch
	left := int64(binary.BigEndian.Uint32(raw[:4])) - now.Unix()
	copy(generation[:], raw[8:idTokenFieldsLen])
	return generation, tagOK && epochOK && left >= 0 && left <= int64((idTokenTTL+clockSkew)/time.Second)
}

func (t IDTokens) tag(id core.ID, fields []byte) []byte {
	mac := t.ring.MAC(pepper.IDToken, append(id[:], fields...)).MAC
	return mac[:idTokenTagLength]
}
