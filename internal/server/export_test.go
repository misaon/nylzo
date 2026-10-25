package server

import (
	"context"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/diskspace"
)

const (
	GlobalCreatesKey = globalCreatesKey
	GlobalStoredKey  = globalStoredKey
)

func ParseCreateHeaderForTest(cfg config.Server, value string) bool {
	s := &Server{cfg: cfg}
	r, _ := http.NewRequest("POST", "/api/v1/messages", nil)
	r.Header.Set(headerCreate, value)
	_, ok := s.parseCreateHeader(r)
	return ok
}

func SolvePoW(challenge string, id core.ID, difficulty int) (string, error) {
	token, _, err := core.SolvePoW(context.Background(), challenge, id, difficulty)
	return token, err
}

func (s *Server) IDTokenForTest(id core.ID) string {
	token, _ := s.idTokens.Issue(id, s.clock())
	return token
}

func (s *Server) ChallengeForTest() [core.ChallengeLen]byte {
	challenge, _ := s.challenges.Issue(s.clock())
	return challenge
}

func (s *Server) ChallengeAtForTest(now time.Time) [core.ChallengeLen]byte {
	challenge, _ := s.challenges.Issue(now)
	return challenge
}

func (s *Server) SetFreeSpaceForTest(free diskspace.FreeFunc) {
	s.disk = diskspace.NewReserver(s.cfg.DataDir, s.cfg.MinFreeBytes, free, s.log)
}
