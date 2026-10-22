package pepper_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/misaon/nylzo/internal/core/vectors"
	"github.com/misaon/nylzo/internal/pepper"
)

func secret(b byte) []byte {
	return bytes.Repeat([]byte{b}, 32)
}

func TestPurposeKeysDiffer(t *testing.T) {
	k, err := pepper.New(secret(1))
	if err != nil {
		t.Fatal(err)
	}
	in := []byte("same input")
	seen := map[[32]byte]pepper.Purpose{}
	for _, p := range []pepper.Purpose{pepper.Read, pepper.Verifier, pepper.Manage, pepper.Lease, pepper.PoW, pepper.ID, pepper.Challenge} {
		m := k.MAC(p, in)
		if prev, dup := seen[m]; dup {
			t.Fatalf("purpose %s and %s share a mac", prev, p)
		}
		seen[m] = p
	}
	other, _ := pepper.New(secret(2))
	if other.MAC(pepper.Read, in) == k.MAC(pepper.Read, in) {
		t.Fatal("different peppers share a mac")
	}
	if other.ID() == k.ID() {
		t.Fatal("the fixed test peppers must have distinct ids")
	}
}

func TestRingVerifyAndRotation(t *testing.T) {
	old, _ := pepper.NewRing(secret(1), nil)
	mac, id := old.MAC(pepper.Read, []byte("token"))
	if !old.Verify(pepper.Read, []byte("token"), mac[:], id) {
		t.Fatal("own mac rejected")
	}
	if old.Verify(pepper.Read, []byte("other"), mac[:], id) {
		t.Fatal("wrong input accepted")
	}
	if old.Verify(pepper.Verifier, []byte("token"), mac[:], id) {
		t.Fatal("wrong purpose accepted")
	}
	rotated, err := pepper.NewRing(secret(2), secret(1))
	if err != nil {
		t.Fatal(err)
	}
	if !rotated.Verify(pepper.Read, []byte("token"), mac[:], id) {
		t.Fatal("previous pepper mac rejected after rotation")
	}
	newMac, newID := rotated.MAC(pepper.Read, []byte("token"))
	if newID == id || newMac == mac {
		t.Fatal("rotation did not change the writing key")
	}
	dropped, _ := pepper.NewRing(secret(2), nil)
	if dropped.Verify(pepper.Read, []byte("token"), mac[:], id) {
		t.Fatal("mac verified after the old pepper was dropped")
	}
	if !dropped.Verify(pepper.Read, []byte("token"), newMac[:], newID) {
		t.Fatal("current mac rejected")
	}
	if dropped.Verify(pepper.Read, []byte("token"), dropped.Dummy(), newID) {
		t.Fatal("dummy accepted")
	}
	if dropped.Verify(pepper.Read, []byte("token"), dropped.Dummy(), id) {
		t.Fatal("dummy accepted for unknown pepper id")
	}
}

func TestRingRejectsCollisionAndBadSecrets(t *testing.T) {
	if _, err := pepper.NewRing(secret(1), secret(1)); !errors.Is(err, pepper.ErrCollision) {
		t.Fatalf("collision: %v", err)
	}
	if _, err := pepper.NewRing(secret(1)[:31], nil); !errors.Is(err, pepper.ErrSecret) {
		t.Fatalf("short secret: %v", err)
	}
	if _, err := pepper.ParseSecret("zz"); !errors.Is(err, pepper.ErrSecret) {
		t.Fatal("bad secret parsed")
	}
	b, err := pepper.ParseSecret(hex.EncodeToString(secret(3)))
	if err != nil || !bytes.Equal(b, secret(3)) {
		t.Fatal("hex secret")
	}
	b, err = pepper.ParseSecret("AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM")
	if err != nil || !bytes.Equal(b, secret(3)) {
		t.Fatalf("base64url secret: %v", err)
	}
}

func TestServerVectors(t *testing.T) {
	data, err := os.ReadFile("../../spec/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var f vectors.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	cur, _ := hex.DecodeString(f.Server.Pepper)
	prev, _ := hex.DecodeString(f.Server.PreviousPepper)
	ring, err := pepper.NewRing(cur, prev)
	if err != nil {
		t.Fatal(err)
	}
	if int(ring.CurrentID()) != f.Server.PepperID {
		t.Fatal("pepper id mismatch")
	}
	for _, c := range f.Server.MACs {
		in, _ := hex.DecodeString(c.Input)
		mac, _ := ring.MAC(pepper.Purpose(c.Purpose), in)
		if hex.EncodeToString(mac[:]) != c.MAC {
			t.Fatalf("mac %s mismatch", c.Purpose)
		}
	}
}
