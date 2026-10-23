package server

import (
	"context"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

const (
	GlobalCreatesKey = globalCreatesKey
	GlobalStoredKey  = globalStoredKey
)

func ClientAddrForTest(cfg Config, mode, remote, xff, realIP string) netip.Addr {
	cfg.ProxyMode = mode
	s := &Server{cfg: cfg}
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	for value := range strings.SplitSeq(realIP, "\n") {
		if value != "" {
			r.Header.Add("X-Real-IP", value)
		}
	}
	return s.clientAddr(r)
}

func ParseCreateHeaderForTest(cfg Config, value string) bool {
	s := &Server{cfg: cfg}
	r, _ := http.NewRequest("POST", "/api/v1/messages", nil)
	r.Header.Set(headerCreate, value)
	_, ok := s.parseCreateHeader(r)
	return ok
}

func PoWSatisfiesForTest(token string, id core.ID, difficulty int) bool {
	chalStr, counterStr, _ := strings.Cut(token, ".")
	chal, err := core.DecodeExact(chalStr, core.PoWChallengeLen)
	if err != nil {
		return false
	}
	counter, err := hex.DecodeString(counterStr)
	if err != nil {
		return false
	}
	return powSatisfies(chal, counter, id, difficulty)
}

func SolvePoW(challenge string, id core.ID, difficulty int) (string, error) {
	token, _, err := core.SolvePoW(context.Background(), challenge, id, difficulty)
	return token, err
}

func (s *Server) IDTokenForTest(id core.ID) string {
	return s.idToken(id, s.clock())
}

func (s *Server) ChallengeForTest() [core.ChallengeLen]byte {
	c, _ := s.issueChallenge(s.clock())
	return c
}

func (s *Server) ChallengeAtForTest(now time.Time) [core.ChallengeLen]byte {
	c, _ := s.issueChallenge(now)
	return c
}

func WithWebFiles(files fs.FS) Option {
	return func(s *Server) {
		if files != nil {
			s.webFiles = files
		}
	}
}
