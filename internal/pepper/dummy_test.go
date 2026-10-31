package pepper

import (
	"bytes"
	"testing"
)

func TestTheDummyMACNeverVerifies(t *testing.T) {
	t.Parallel()
	dropped, err := newKey(bytes.Repeat([]byte{1}, SecretLen))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := NewRing(bytes.Repeat([]byte{2}, SecretLen), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, pepperID := range []byte{ring.CurrentID(), dropped.id} {
		if ring.Verify(Read, []byte("token"), ring.dummy[:], pepperID) {
			t.Fatalf("the dummy mac verified for pepper id %d", pepperID)
		}
	}
}
