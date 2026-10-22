package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

const (
	challengeTTL      = time.Minute
	challengeNonceLen = 16
	challengeSigned   = 4 + challengeNonceLen
	challengeSpentKey = "challenge:"
)

type challengeResponse struct {
	Challenge string `json:"challenge"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	c, expires := s.issueChallenge(s.clock())
	writeJSON(w, http.StatusOK, challengeResponse{Challenge: core.Encode(c[:]), ExpiresAt: formatTime(expires)})
}

func (s *Server) issueChallenge(now time.Time) ([core.ChallengeLen]byte, time.Time) {
	var c [core.ChallengeLen]byte
	expires := now.Add(challengeTTL).Truncate(time.Second)
	binary.BigEndian.PutUint32(c[:4], uint32(expires.Unix()))
	rand.Read(c[4:challengeSigned])
	copy(c[challengeSigned:], s.challengeTag(c[:challengeSigned]))
	return c, expires
}

func (s *Server) challengeTag(signed []byte) []byte {
	mac, _ := s.ring.MAC(pepper.Challenge, signed)
	return mac[:core.ChallengeLen-challengeSigned]
}

func (s *Server) validChallenge(c [core.ChallengeLen]byte, now time.Time) bool {
	tagOK := hmac.Equal(s.challengeTag(c[:challengeSigned]), c[challengeSigned:])
	left := int64(binary.BigEndian.Uint32(c[:4])) - now.Unix()
	return tagOK && left >= 0 && left <= int64((challengeTTL+powClockSkew)/time.Second)
}

func (s *Server) spendChallenge(ctx context.Context, c [core.ChallengeLen]byte) (bool, error) {
	expires := time.Unix(int64(binary.BigEndian.Uint32(c[:4])), 0)
	_, ok, err := s.store.RateReserve(context.WithoutCancel(ctx), store.RateHit{
		Key:         challengeSpentKey + hex.EncodeToString(c[4:challengeSigned]),
		WindowStart: time.Unix(0, 0),
		PurgeAt:     store.CeilMinute(expires.Add(challengeTTL)),
		Limit:       1,
	})
	return ok, err
}
