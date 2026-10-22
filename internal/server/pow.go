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

	"golang.org/x/crypto/scrypt"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	powN          = 1 << 14
	powR          = 8
	powP          = 1
	powWindow     = 120 * time.Second
	powNonceLen   = 16
	powTagLen     = 16
	powCounterLen = 8
	powChalLen    = 4 + powNonceLen + powTagLen
)

var ErrPoW = errors.New("server: invalid proof of work")

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
	if err != nil || len(counter) != powCounterLen {
		return ErrPoW
	}
	tag, _ := s.ring.MAC(pepper.PoW, chal[:4+powNonceLen])
	if !hmac.Equal(tag[:powTagLen], chal[4+powNonceLen:]) {
		return ErrPoW
	}
	issued := int64(binary.BigEndian.Uint32(chal[0:4]))
	age := now.Unix() - issued
	if age < 0 || age > int64(powWindow/time.Second) {
		return ErrPoW
	}
	select {
	case s.powSlots <- struct{}{}:
		defer func() { <-s.powSlots }()
	default:
		return errPoWBusy
	}
	password := make([]byte, 0, powNonceLen+core.IDLen)
	password = append(password, chal[4:4+powNonceLen]...)
	password = append(password, id[:]...)
	out, err := scrypt.Key(password, counter, powN, powR, powP, 32)
	if err != nil {
		return ErrPoW
	}
	if !leadingZeroBits(out, s.cfg.PoWDifficulty) {
		return ErrPoW
	}
	return nil
}

var errPoWBusy = errors.New("server: proof-of-work verifier is saturated")

func leadingZeroBits(b []byte, bits int) bool {
	for i := 0; i < bits; i++ {
		if b[i/8]&(0x80>>(i%8)) != 0 {
			return false
		}
	}
	return true
}

func SolvePoW(challenge string, id core.ID, difficulty int) (string, error) {
	chal, err := core.DecodeExact(challenge, powChalLen)
	if err != nil {
		return "", err
	}
	password := append(append([]byte(nil), chal[4:4+powNonceLen]...), id[:]...)
	counter := make([]byte, powCounterLen)
	for i := uint64(0); ; i++ {
		binary.BigEndian.PutUint64(counter, i)
		out, err := scrypt.Key(password, counter, powN, powR, powP, 32)
		if err != nil {
			return "", err
		}
		if leadingZeroBits(out, difficulty) {
			return challenge + "." + hex.EncodeToString(counter), nil
		}
	}
}
