package server

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func powServer(t *testing.T, concurrency int) *Server {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := Defaults()
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	cfg.PoWConcurrency = concurrency
	cfg.PoWDifficulty = 1
	s, err := New(cfg, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPoWVerifierSaturated(t *testing.T) {
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
	if err := s.powVerify(t.Context(), token, id, now); err != nil {
		t.Fatalf("a busy answer must not use up the challenge: %v", err)
	}
	if err := s.powVerify(t.Context(), token, id, now); !errors.Is(err, ErrPoW) {
		t.Fatalf("replayed token: %v", err)
	}
}

func TestPoWRejectsFutureChallenge(t *testing.T) {
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
	typ := reflect.TypeFor[createHeader]()
	tags := map[string]bool{}
	for i := range typ.NumField() {
		tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		tags[tag] = true
	}
	if !reflect.DeepEqual(tags, createHeaderKeys) {
		t.Fatalf("createHeaderKeys %v differ from the createHeader tags %v", createHeaderKeys, tags)
	}
}
