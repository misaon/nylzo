package localclient

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

const maxTokenBytes = 64

type handoff struct {
	mutex   sync.Mutex
	token   []byte
	payload []byte
	expires time.Time
}

type handedOver struct {
	Path     string `json:"path"`
	Fragment string `json:"fragment"`
}

// Offer keeps a link for one redemption before the deadline; the page receives it in memory, never in an address.
func (s *Server) Offer(path, fragment string, lifetime time.Duration) (string, error) {
	payload, err := json.Marshal(handedOver{Path: path, Fragment: fragment})
	if err != nil {
		return "", err
	}
	token := rand.Text()
	s.handoff.mutex.Lock()
	defer s.handoff.mutex.Unlock()
	s.handoff.forget()
	s.handoff.token, s.handoff.payload, s.handoff.expires = []byte(token), payload, time.Now().Add(lifetime)
	return token, nil
}

func (s *Server) Withdraw() {
	s.handoff.mutex.Lock()
	defer s.handoff.mutex.Unlock()
	s.handoff.forget()
}

func (s *Server) handleRedeem(w http.ResponseWriter, r *http.Request) {
	token, err := io.ReadAll(io.LimitReader(r.Body, maxTokenBytes+1))
	payload, ok := s.handoff.take(token, time.Now())
	if err != nil || !ok {
		s.notify(HandoffRefused)
		writeJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	defer clear(payload)
	s.notify(HandoffTaken)
	w.Header().Set("Content-Type", jsonContentType)
	w.Write(payload)
}

func (h *handoff) take(token []byte, now time.Time) ([]byte, bool) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.payload != nil && now.After(h.expires) {
		h.forget()
	}
	if h.payload == nil || subtle.ConstantTimeCompare(token, h.token) != 1 {
		return nil, false
	}
	payload := h.payload
	h.payload = nil
	h.forget()
	return payload, true
}

func (h *handoff) forget() {
	clear(h.token)
	clear(h.payload)
	h.token, h.payload = nil, nil
}
