package server

import (
	"net/http"
	"net/netip"
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
