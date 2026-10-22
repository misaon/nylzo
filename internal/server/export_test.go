package server

import (
	"encoding/hex"
	"net/http"
	"net/netip"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

func ClientAddrForTest(cfg Config, mode, remote, xff, realIP string) netip.Addr {
	cfg.ProxyMode = mode
	s := &Server{cfg: cfg}
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	if realIP != "" {
		r.Header.Set("X-Real-IP", realIP)
	}
	return s.clientAddr(r)
}

func ParseCreateHeaderForTest(cfg Config, value string) bool {
	s := &Server{cfg: cfg}
	r, _ := http.NewRequest("POST", "/api/v1/messages", nil)
	r.Header.Set(headerCreate, value)
	_, _, _, _, _, ok := s.parseCreateHeader(r)
	return ok
}

func PoWSatisfiesForTest(token string, id core.ID, difficulty int) bool {
	chalStr, counterStr, _ := strings.Cut(token, ".")
	chal, err := core.DecodeExact(chalStr, powChalLen)
	if err != nil {
		return false
	}
	counter, err := hex.DecodeString(counterStr)
	if err != nil {
		return false
	}
	return powSatisfies(chal, counter, id, difficulty)
}
