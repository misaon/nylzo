package server_test

import (
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
)

func TestAK27PepperRotation(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	old := newMessage(t, core.KDFPBKDF2, "pw", 2, 7*24*3600, "before rotation")
	h.mustCreate(old)
	h.reopen(baseConfig(pepperB, pepperA))
	if resp, _ := h.metadata(old); resp.StatusCode != 200 {
		t.Fatalf("old message after rotation: %d", resp.StatusCode)
	}
	resp, _ := h.claim(old, old.verifier, "")
	if resp.StatusCode != 200 {
		t.Fatalf("claim old message after rotation: %d", resp.StatusCode)
	}
	if resp, _ := h.status(old); resp.StatusCode != 200 {
		t.Fatal("status with old manage mac")
	}
	fresh := newMessage(t, core.KDFNone, "", 1, 3600, "after rotation")
	h.mustCreate(fresh)
	h.reopen(baseConfig(pepperB, nil))
	if resp, _ := h.metadata(fresh); resp.StatusCode != 200 {
		t.Fatal("new message must verify with the new pepper alone")
	}
	if resp, _ := h.metadata(old); resp.StatusCode != 404 {
		t.Fatalf("message under a dropped pepper must look unknown: %d", resp.StatusCode)
	}
	if _, err := server.New(baseConfig(pepperA, pepperA), h.store, h.blobs); err == nil {
		t.Fatal("identical peppers accepted")
	}
}
