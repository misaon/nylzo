package proof

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/core/vectors"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func testRing(t *testing.T) *pepper.Ring {
	t.Helper()
	ring, err := pepper.NewRing(bytes.Repeat([]byte{1}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()
	storage, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	return storage
}

func solve(t *testing.T, challenge string, id core.ID) string {
	t.Helper()
	token, err := proofofwork.SolvePoW(t.Context(), challenge, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestWorkWaitsForAVerifierSlot(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		work := NewWork(testRing(t), openStore(t), 1, 1)
		now := time.Now()
		id := core.NewID()
		token := solve(t, work.Challenge(now), id)
		work.slots <- struct{}{}
		if err := work.Verify(t.Context(), token, id, now); !errors.Is(err, ErrWorkBusy) {
			t.Fatalf("saturated verifier: want ErrWorkBusy, got %v", err)
		}
		<-work.slots
		if err := work.Verify(t.Context(), token, id, now); !errors.Is(err, ErrWork) {
			t.Fatalf("a challenge is used once it is recorded, even after a busy answer: %v", err)
		}
		fresh := solve(t, work.Challenge(now), id)
		work.slots <- struct{}{}
		go func() {
			time.Sleep(10 * time.Millisecond)
			<-work.slots
		}()
		if err := work.Verify(t.Context(), fresh, id, now); err != nil {
			t.Fatalf("a slot that frees up within the wait must be used: %v", err)
		}
		gone := solve(t, work.Challenge(now), id)
		work.slots <- struct{}{}
		defer func() { <-work.slots }()
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		if err := work.Verify(ctx, gone, id, now); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("a client that went away must stop waiting for a slot: %v", err)
		}
	})
}

func TestWorkTokensFollowTheSpec(t *testing.T) {
	t.Parallel()
	work := NewWork(testRing(t), openStore(t), 1, 1)
	now := time.Now()
	id := core.NewID()
	verify := func(token string, tokenID core.ID) error { return work.Verify(t.Context(), token, tokenID, now) }
	for _, window := range []struct {
		issued time.Duration
		want   error
	}{{time.Minute, ErrWork}, {-121 * time.Second, ErrWork}, {2 * time.Second, nil}} {
		if err := verify(solve(t, work.Challenge(now.Add(window.issued)), id), id); !errors.Is(err, window.want) {
			t.Errorf("a challenge issued %v from now: %v, want %v", window.issued, err, window.want)
		}
	}
	challenge := work.Challenge(now)
	token, _ := work.parse(challenge+".00000000abcdef00", now)
	for !token.satisfies(id, 1) {
		token.counter++
	}
	lower := hex.EncodeToString(binary.BigEndian.AppendUint64(nil, token.counter))
	for _, step := range []struct {
		counter string
		want    error
	}{{strings.ToUpper(lower), ErrWork}, {lower, nil}, {lower, ErrWork}} {
		if err := verify(challenge+"."+step.counter, id); !errors.Is(err, step.want) {
			t.Fatalf("counter %s: %v, want %v", step.counter, err, step.want)
		}
	}
	forged := make([]byte, proofofwork.PoWChallengeLen)
	binary.BigEndian.PutUint32(forged, unixSeconds(now))
	if err := verify(solve(t, core.Encode(forged), id), id); !errors.Is(err, ErrWork) {
		t.Fatalf("a challenge with a forged tag: %v", err)
	}
	for _, malformed := range []string{"no-dot", challenge + ".00", challenge + ".000000000000000g"} {
		if err := verify(malformed, id); !errors.Is(err, ErrWork) {
			t.Fatalf("malformed token %q: %v", malformed, err)
		}
	}
	other := solve(t, work.Challenge(now), id)
	otherToken, _ := work.parse(other, now)
	otherID := core.NewID()
	for otherToken.satisfies(otherID, 1) {
		otherID = core.NewID()
	}
	if err := verify(other, otherID); !errors.Is(err, ErrWork) {
		t.Fatalf("a token solved for another id: %v", err)
	}
}

func TestWorkMatchesTheVectors(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../spec/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file vectors.File
	if decodeErr := json.Unmarshal(data, &file); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	for _, vector := range file.PoW {
		decoded, decodeErr := core.DecodeExact(vector.Challenge, proofofwork.PoWChallengeLen)
		id, idErr := core.ParseID(vector.ID)
		if decodeErr != nil || idErr != nil {
			t.Fatalf("%s: %v %v", vector.Name, decodeErr, idErr)
		}
		token := workToken{challenge: (*[proofofwork.PoWChallengeLen]byte)(decoded), counter: vector.Counter}
		if !token.satisfies(id, vector.ZeroBits) || token.satisfies(id, vector.ZeroBits+1) {
			t.Fatalf("%s: the verifier does not count exactly %d zero bits", vector.Name, vector.ZeroBits)
		}
	}
}

func TestIDTokensFollowTheSpec(t *testing.T) {
	t.Parallel()
	tokens := NewIDTokens(testRing(t))
	now := time.Now()
	id := core.NewID()
	ahead, _ := tokens.Issue(id, now.Add(5*time.Second))
	if !tokens.Valid(id, ahead, now) {
		t.Fatal("a token from an instance 5 s ahead must pass")
	}
	future, _ := tokens.Issue(id, now.Add(time.Minute))
	expired, _ := tokens.Issue(id, now.Add(-idTokenTTL-time.Second))
	if tokens.Valid(id, future, now) || tokens.Valid(id, expired, now) || tokens.Valid(core.NewID(), ahead, now) {
		t.Fatal("a token outside its window or for another id was accepted")
	}
}
