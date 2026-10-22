package server

import (
	"crypto/hmac"
	"encoding/binary"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	idTokenTTL    = 10 * time.Minute
	idTokenTagLen = 16
	idTokenLen    = 4 + idTokenTagLen
)

func (s *Server) issueID(now time.Time) instanceCreate {
	id := core.NewID()
	expires := now.Add(idTokenTTL).Truncate(time.Second)
	return instanceCreate{ID: id.String(), IDToken: s.idToken(id, now), ExpiresAt: formatTime(expires)}
}

func (s *Server) idToken(id core.ID, now time.Time) string {
	token := make([]byte, idTokenLen)
	binary.BigEndian.PutUint32(token, uint32(now.Add(idTokenTTL).Unix()))
	copy(token[4:], s.idTokenTag(id, token[:4]))
	return core.Encode(token)
}

func (s *Server) idTokenTag(id core.ID, expires []byte) []byte {
	mac, _ := s.ring.MAC(pepper.ID, append(id[:], expires...))
	return mac[:idTokenTagLen]
}

func (s *Server) validIDToken(id core.ID, token string, now time.Time) bool {
	raw, err := core.DecodeExact(token, idTokenLen)
	if err != nil {
		return false
	}
	tagOK := hmac.Equal(s.idTokenTag(id, raw[:4]), raw[4:])
	left := int64(binary.BigEndian.Uint32(raw[:4])) - now.Unix()
	return tagOK && left >= 0 && left <= int64((idTokenTTL+powClockSkew)/time.Second)
}
