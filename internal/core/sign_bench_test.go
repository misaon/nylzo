package core_test

import (
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func BenchmarkValidPublicKey(b *testing.B) {
	pk := core.PublicKey(core.NewSecret())
	for b.Loop() {
		core.ValidPublicKey(pk[:])
	}
}

func BenchmarkVerifyRequest(b *testing.B) {
	var challenge [core.ChallengeLen]byte
	sig := core.SignRequest(core.NewSecret(), core.PurposeRead, "GET", "/api/v1/messages/AAAAAAAAAAAAAAAAAAAAAA", challenge, nil)
	for b.Loop() {
		if !sig.Verify(core.PurposeRead, "GET", "/api/v1/messages/AAAAAAAAAAAAAAAAAAAAAA", nil) {
			b.Fatal("signature does not verify")
		}
	}
}
