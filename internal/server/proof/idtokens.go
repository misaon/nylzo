package proof

import (
	"bytes"
	"crypto/hmac"
	"encoding/binary"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	idTokenTTL       = api.IDTokenLifetime
	idTokenTagLength = 16
	idTokenFieldsLen = 12 + epoch.GenerationIDLen
	idTokenLength    = idTokenFieldsLen + idTokenTagLength
)

type IDTokens struct {
	ring *pepper.Ring
}

func NewIDTokens(ring *pepper.Ring) IDTokens {
	return IDTokens{ring: ring}
}

// Issue binds the id to the epoch of its expiry share, which fixes its deadline, to the TTL the deadline came from and
// to the generation whose seed made the share (spec section 5.4).
func (t IDTokens) Issue(
	id core.ID,
	shareEpoch, ttlSeconds uint32,
	generation epoch.GenerationID,
	now time.Time,
) (string, time.Time) {
	expires := now.Add(idTokenTTL)
	token := make([]byte, idTokenLength)
	binary.BigEndian.PutUint32(token, unixSeconds(expires))
	binary.BigEndian.PutUint32(token[4:], shareEpoch)
	binary.BigEndian.PutUint32(token[8:], ttlSeconds)
	copy(token[12:idTokenFieldsLen], generation[:])
	copy(token[idTokenFieldsLen:], t.tag(id, token[:idTokenFieldsLen]))
	return core.Encode(token), expires.Truncate(time.Second)
}

// Valid also returns the generation the token names, so a create can tell a share of a lost seed apart. A token whose
// deadline has passed is refused, so no message is stored after its share has gone.
func (t IDTokens) Valid(
	id core.ID,
	shareEpoch, ttlSeconds uint32,
	token string,
	now time.Time,
) (epoch.GenerationID, bool) {
	var generation epoch.GenerationID
	raw, err := core.DecodeExact(token, idTokenLength)
	if err != nil {
		return generation, false
	}
	fields := binary.BigEndian.AppendUint32(bytes.Clone(raw[:4]), shareEpoch)
	fields = binary.BigEndian.AppendUint32(fields, ttlSeconds)
	fields = append(fields, raw[12:idTokenFieldsLen]...)
	tagOK := hmac.Equal(t.tag(id, fields), raw[idTokenFieldsLen:])
	claimsOK := bytes.Equal(fields, raw[:idTokenFieldsLen])
	left := int64(binary.BigEndian.Uint32(raw[:4])) - now.Unix()
	copy(generation[:], raw[12:idTokenFieldsLen])
	return generation, tagOK && claimsOK && left >= 0 && left <= int64((idTokenTTL+clockSkew)/time.Second) &&
		now.Before(epoch.Deadline(shareEpoch))
}

func (t IDTokens) tag(id core.ID, fields []byte) []byte {
	mac := t.ring.MAC(pepper.IDToken, append(id[:], fields...)).MAC
	return mac[:idTokenTagLength]
}
