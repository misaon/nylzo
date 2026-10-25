package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestPassphraseIsCheckedBeforeDerivation(t *testing.T) {
	t.Parallel()
	var salt [32]byte
	if key, err := core.DerivePassphraseKey(core.KDFNone, "caf\xe9", &salt); err != nil || key != nil {
		t.Fatalf("profile 0 must ignore any passphrase: %x %v", key, err)
	}
	if _, err := core.DerivePassphraseKey(core.KDF(3), "x", &salt); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("an unknown profile: %v", err)
	}
	_, err := core.DerivePassphraseKey(core.KDFScrypt, "caf\xe9", &salt)
	if !errors.Is(err, core.ErrMalformedPassphrase) {
		t.Fatalf("scrypt accepted invalid UTF-8: %v", err)
	}
	link, _, err := core.DeriveAll(core.NewID(), core.NewSecret(), core.KDFScrypt, "caf\xe9")
	if !errors.Is(err, core.ErrMalformedPassphrase) || link != (core.LinkKeys{}) {
		t.Fatalf("DeriveAll must refuse before deriving anything: %v", err)
	}
}

func TestRetiredProfileIsRefusedEverywhere(t *testing.T) {
	t.Parallel()
	secret := core.Encode(make([]byte, core.SecretLen))
	_, err := core.ParseLink("https://nylzo.example/m/" + core.NewID().String() + "#1.2." + secret)
	if !errors.Is(err, core.ErrLinkRetiredKDF) {
		t.Fatalf("a link with kdf_id 2: %v", err)
	}
	if _, err = core.ParseKDF("2"); !errors.Is(err, core.ErrKDF) || core.KDFRetired.Valid() {
		t.Fatalf("ParseKDF accepted the retired profile: %v", err)
	}
	var salt [32]byte
	passphrase := "correct horse battery staple"
	key, err := core.DerivePassphraseKey(core.KDFRetired, passphrase, &salt)
	if !errors.Is(err, core.ErrKDF) || key != nil {
		t.Fatalf("derived a key for the retired profile: %v", err)
	}
	_, _, err = core.DeriveAll(core.NewID(), core.NewSecret(), core.KDFRetired, passphrase)
	if !errors.Is(err, core.ErrKDF) {
		t.Fatalf("DeriveAll accepted the retired profile: %v", err)
	}
	header := core.NewHeader(core.KDFScrypt).Bytes()
	header[1] = byte(core.KDFRetired)
	if _, err = core.ParseHeader(header); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("an envelope header with kdf_id 2: %v", err)
	}
	payload := core.Payload{Meta: core.Meta{Text: new("x")}}
	policy := core.Policy{OpensMax: 1, TTLSeconds: 60}
	_, err = core.Seal(new([core.KeyLen]byte), core.NewHeader(core.KDFRetired), core.NewID(), policy, payload)
	if !errors.Is(err, core.ErrKDF) {
		t.Fatalf("sealed an envelope for the retired profile: %v", err)
	}
}

func TestAK24ServerProfileMustMatchTheFragment(t *testing.T) {
	t.Parallel()
	for _, reported := range []int{int(core.KDFNone), int(core.KDFRetired), 3, 257, -255} {
		err := core.AssertProfile(core.KDFScrypt, reported)
		if !errors.Is(err, core.ErrKDFMismatch) || strings.Contains(err.Error(), "envelope") {
			t.Fatalf("server-reported kdf_id %d: %v", reported, err)
		}
	}
	if err := core.AssertProfile(core.KDFScrypt, int(core.KDFScrypt)); err != nil {
		t.Fatal(err)
	}
}
