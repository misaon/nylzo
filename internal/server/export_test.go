package server

import (
	"context"
	"time"

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/diskspace"
)

const (
	GlobalCreatesKey = globalCreatesKey
	GlobalStoredKey  = globalStoredKey
)

func ParseCreateHeaderForTest(settings config.Server, value string) bool {
	_, ok := parseCreateHeader(value, settings.Limits)
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
	s.disk = diskspace.NewReserver(s.settings.DataDir, s.settings.MinFreeBytes, free, s.logger)
}
