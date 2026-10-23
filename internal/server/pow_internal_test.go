package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func powServer(t *testing.T, concurrency int) *Server {
	t.Helper()
	st, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := Defaults()
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	cfg.PoWConcurrency = concurrency
	cfg.PoWDifficulty = 1
	s, err := New(cfg, st, nil, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPoWVerifierSaturated(t *testing.T) {
	t.Parallel()
	synctest.Test(t, testPoWVerifierSaturated)
}

func testPoWVerifierSaturated(t *testing.T) {
	s := powServer(t, 1)
	now := time.Now()
	id := core.NewID()
	token, err := SolvePoW(s.powChallenge(now), id, s.cfg.PoWDifficulty)
	if err != nil {
		t.Fatal(err)
	}
	s.powSlots <- struct{}{}
	if err := s.powVerify(t.Context(), token, id, now); !errors.Is(err, errPoWBusy) {
		t.Fatalf("saturated verifier: want errPoWBusy, got %v", err)
	}
	<-s.powSlots
	if err := s.powVerify(t.Context(), token, id, now); !errors.Is(err, ErrPoW) {
		t.Fatalf("a challenge is used once it is recorded, even after a busy answer: %v", err)
	}
	fresh, err := SolvePoW(s.powChallenge(now), id, s.cfg.PoWDifficulty)
	if err != nil {
		t.Fatal(err)
	}
	s.powSlots <- struct{}{}
	go func() {
		time.Sleep(10 * time.Millisecond)
		<-s.powSlots
	}()
	if err := s.powVerify(t.Context(), fresh, id, now); err != nil {
		t.Fatalf("a slot that frees up within the wait must be used: %v", err)
	}
	gone, err := SolvePoW(s.powChallenge(now), id, s.cfg.PoWDifficulty)
	if err != nil {
		t.Fatal(err)
	}
	s.powSlots <- struct{}{}
	defer func() { <-s.powSlots }()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := s.powVerify(ctx, gone, id, now); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a client that went away must stop waiting for a slot: %v", err)
	}
}

func TestPoWRejectsFutureChallenge(t *testing.T) {
	t.Parallel()
	s := powServer(t, 1)
	now := time.Now()
	id := core.NewID()
	token, err := SolvePoW(s.powChallenge(now.Add(time.Minute)), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.powVerify(t.Context(), token, id, now); !errors.Is(err, ErrPoW) {
		t.Fatalf("future challenge: %v", err)
	}
}

func TestPoWRejectsUppercaseCounter(t *testing.T) {
	t.Parallel()
	s := powServer(t, 1)
	now := time.Now()
	id := core.NewID()
	challenge := s.powChallenge(now)
	chal, err := core.DecodeExact(challenge, powChalLen)
	if err != nil {
		t.Fatal(err)
	}
	var counter []byte
	for i := uint64(0xabcdef00); ; i++ {
		if counter = binary.BigEndian.AppendUint64(nil, i); powSatisfies(chal, counter, id, 1) {
			break
		}
	}
	lower := hex.EncodeToString(counter)
	if err := s.powVerify(t.Context(), challenge+"."+strings.ToUpper(lower), id, now); !errors.Is(err, ErrPoW) {
		t.Fatalf("uppercase counter: %v", err)
	}
	if err := s.powVerify(t.Context(), challenge+"."+lower, id, now); err != nil {
		t.Fatalf("lowercase token after the rejected variant: %v", err)
	}
}

func TestCreateHeaderKeysMatchStruct(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[createHeader]()
	tags := map[string]bool{}
	for field := range typ.Fields() {
		tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		tags[tag] = true
	}
	if !reflect.DeepEqual(tags, createHeaderKeys) {
		t.Fatalf("createHeaderKeys %v differ from the createHeader tags %v", createHeaderKeys, tags)
	}
}

func TestPoWToleratesASlightlyAheadIssuer(t *testing.T) {
	t.Parallel()
	s := powServer(t, 1)
	now := time.Now()
	id := core.NewID()
	token, err := SolvePoW(s.powChallenge(now.Add(2*time.Second)), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.powVerify(t.Context(), token, id, now); err != nil {
		t.Fatalf("a challenge from an instance 2 s ahead must verify: %v", err)
	}
}
