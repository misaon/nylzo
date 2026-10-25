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

func secret(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, pepper.SecretLen)
}

func newRing(t *testing.T, current, previous []byte) *pepper.Ring {
	t.Helper()
	ring, err := pepper.NewRing(current, previous)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func TestEveryPurposeHasItsOwnKey(t *testing.T) {
	ring := newRing(t, secret(1), nil)
	seen := map[[pepper.MACLen]byte]pepper.Purpose{}
	purposes := []pepper.Purpose{
		pepper.Read, pepper.Verifier, pepper.Manage, pepper.Lease, pepper.PoW, pepper.IDToken, pepper.Challenge,
	}
	for _, purpose := range purposes {
		mac := ring.MAC(purpose, []byte("same input")).MAC
		if other, duplicate := seen[mac]; duplicate {
			t.Fatalf("purposes %s and %s share a mac", other, purpose)
		}
		seen[mac] = purpose
	}
	for _, unknown := range []pepper.Purpose{"", "reed"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("purpose %q got a mac under a key that was never derived", unknown)
				}
			}()
			ring.MAC(unknown, []byte("x"))
		}()
	}
}

func TestAK27RotationKeepsOldMacsValid(t *testing.T) {
	input := []byte("token")
	old := newRing(t, secret(1), nil)
	tag := old.MAC(pepper.Read, input)
	mac, id := tag.MAC, tag.PepperID
	if !old.Verify(pepper.Read, input, mac[:], id) || old.Verify(pepper.Read, []byte("other"), mac[:], id) ||
		old.Verify(pepper.Verifier, input, mac[:], id) {
		t.Fatal("a mac verifies only its own input and purpose")
	}
	rotated := newRing(t, secret(2), secret(1))
	if !rotated.Verify(pepper.Read, input, mac[:], id) {
		t.Fatal("the previous pepper's mac was rejected after the rotation")
	}
	rotatedTag := rotated.MAC(pepper.Read, input)
	newMAC, newID := rotatedTag.MAC, rotatedTag.PepperID
	if newID == id || newMAC == mac {
		t.Fatal("the rotation did not change the writing key")
	}
	dropped := newRing(t, secret(2), nil)
	if dropped.Verify(pepper.Read, input, mac[:], id) || !dropped.Verify(pepper.Read, input, newMAC[:], newID) {
		t.Fatal("after dropping the old pepper only the current macs verify")
	}
	for _, pepperID := range []byte{newID, id} {
		if dropped.Verify(pepper.Read, input, dropped.Dummy(), pepperID) {
			t.Fatalf("the dummy mac verified for pepper id %d", pepperID)
		}
	}
}

func TestSecretsAreCheckedBeforeUse(t *testing.T) {
	if _, err := pepper.NewRing(secret(1), secret(1)); !errors.Is(err, pepper.ErrCollision) {
		t.Fatalf("two peppers with one pepper_id: %v", err)
	}
	if _, err := pepper.NewRing(secret(1)[:31], nil); !errors.Is(err, pepper.ErrSecret) {
		t.Fatalf("a short secret: %v", err)
	}
	sequence := make([]byte, pepper.SecretLen)
	for i := range sequence {
		sequence[i] = byte(i)
	}
	for _, text := range []string{hex.EncodeToString(sequence), "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"} {
		if parsed, err := pepper.ParseSecret(text); err != nil || !bytes.Equal(parsed, sequence) {
			t.Fatalf("%q: %x %v", text, parsed, err)
		}
	}
	if _, err := pepper.ParseSecret("zz"); !errors.Is(err, pepper.ErrSecret) {
		t.Fatalf("a malformed secret: %v", err)
	}
	weak := []string{
		hex.EncodeToString(secret(0)),
		hex.EncodeToString(secret(0xab)),
		"AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM",
	}
	for _, text := range weak {
		if _, err := pepper.ParseSecret(text); !errors.Is(err, pepper.ErrWeak) {
			t.Fatalf("a pepper of equal bytes was accepted: %q %v", text, err)
		}
	}
}

func TestServerVectors(t *testing.T) {
	data, err := os.ReadFile("../../spec/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file vectors.File
	if err = json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	current, _ := hex.DecodeString(file.Server.Pepper)
	previous, _ := hex.DecodeString(file.Server.PreviousPepper)
	ring := newRing(t, current, previous)
	if int(ring.CurrentID()) != file.Server.PepperID {
		t.Fatal("pepper id mismatch")
	}
	for _, vector := range file.Server.MACs {
		input, _ := hex.DecodeString(vector.Input)
		mac := ring.MAC(pepper.Purpose(vector.Purpose), input).MAC
		if hex.EncodeToString(mac[:]) != vector.MAC {
			t.Fatalf("mac %s mismatch", vector.Purpose)
		}
	}
}
