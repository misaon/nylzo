package server

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

func TestPoWVerifierSaturated(t *testing.T) {
	cfg := Defaults()
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	cfg.PoWConcurrency = 1
	cfg.PoWDifficulty = 1
	s, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	id := core.NewID()
	token, err := SolvePoW(s.powChallenge(now), id, cfg.PoWDifficulty)
	if err != nil {
		t.Fatal(err)
	}
	s.powSlots <- struct{}{}
	if err := s.powVerify(context.Background(), token, id, now); !errors.Is(err, errPoWBusy) {
		t.Fatalf("saturated verifier: want errPoWBusy, got %v", err)
	}
	<-s.powSlots
	if err := s.powVerify(context.Background(), token, id, now); err != nil {
		t.Fatalf("free verifier: %v", err)
	}
}
