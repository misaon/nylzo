package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

const (
	powWindow     = 120 * time.Second
	powClockSkew  = 5 * time.Second
	powNonceLen   = 16
	powTagLen     = 16
	powCounterLen = 8
	powChalLen    = core.PoWChallengeLen
	powNonceKey   = "pow:"
	powNonceTTL   = 2 * powWindow
)

var ErrPoW = errors.New("server: invalid proof of work")

const powSlotWait = 10 * time.Second

func (s *Server) powChallenge(now time.Time) string {
	buf := make([]byte, powChalLen)
	binary.BigEndian.PutUint32(buf[0:4], uint32(now.Unix()))
	rand.Read(buf[4 : 4+powNonceLen])
	tag, _ := s.ring.MAC(pepper.PoW, buf[:4+powNonceLen])
	copy(buf[4+powNonceLen:], tag[:powTagLen])
	return core.Encode(buf)
}

func (s *Server) powVerify(ctx context.Context, token string, id core.ID, now time.Time) error {
	chalStr, counterStr, ok := strings.Cut(token, ".")
	if !ok || len(counterStr) != 2*powCounterLen {
		return ErrPoW
	}
	chal, err := core.DecodeExact(chalStr, powChalLen)
	if err != nil {
		return ErrPoW
	}
	counter, err := hex.DecodeString(counterStr)
	if err != nil || len(counter) != powCounterLen || strings.ContainsAny(counterStr, "ABCDEF") {
		return ErrPoW
	}
	tag, _ := s.ring.MAC(pepper.PoW, chal[:4+powNonceLen])
	if !hmac.Equal(tag[:powTagLen], chal[4+powNonceLen:]) {
		return ErrPoW
	}
	issued := int64(binary.BigEndian.Uint32(chal[0:4]))
	age := now.Unix() - issued
	if age < -int64(powClockSkew/time.Second) || age > int64(powWindow/time.Second) {
		return ErrPoW
	}
	purge := store.CeilMinute(time.Unix(issued, 0).Add(powNonceTTL))
	used, err := s.store.RateHit(ctx, []store.RateHit{{Key: powNonceKey + hex.EncodeToString(chal[4:4+powNonceLen]), WindowStart: time.Unix(0, 0), PurgeAt: purge}})
	if err != nil {
		return err
	}
	if used[0].Count > 1 {
		return ErrPoW
	}
	wait := time.NewTimer(s.powSlotWait)
	defer wait.Stop()
	select {
	case s.powSlots <- struct{}{}:
		defer func() { <-s.powSlots }()
	case <-wait.C:
		return errPoWBusy
	case <-ctx.Done():
		return ctx.Err()
	}
	if !powSatisfies(chal, counter, id, s.cfg.PoWDifficulty) {
		return ErrPoW
	}
	return nil
}

func powSatisfies(chal, counter []byte, id core.ID, difficulty int) bool {
	digest, err := core.PoWDigest((*[core.PoWChallengeLen]byte)(chal), id, binary.BigEndian.Uint64(counter))
	return err == nil && core.LeadingZeroBits(digest[:]) >= difficulty
}

var errPoWBusy = errors.New("server: proof-of-work verifier is saturated")
