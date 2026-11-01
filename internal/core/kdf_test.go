package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
)

func TestRetiredProfileIsRefusedEverywhere(t *testing.T) {
	t.Parallel()
	secret := core.Encode(make([]byte, core.SecretLen))
	link := "https://nylzo.example/r#2.2." + core.NewID().String() + "." + secret + "." + secret
	if _, err := core.ParseLink(link); !errors.Is(err, core.ErrLinkRetiredKDF) {
		t.Fatalf("a link with kdf_id 2: %v", err)
	}
	if _, err := core.ParseKDF("2"); !errors.Is(err, core.ErrKDF) || core.KDFRetired.Valid() {
		t.Fatalf("ParseKDF accepted the retired profile: %v", err)
	}
	var salt [32]byte
	passphrase := "correct horse battery staple"
	key, err := keys.DerivePassphraseKey(core.KDFRetired, passphrase, &salt)
	if !errors.Is(err, core.ErrKDF) || key != nil {
		t.Fatalf("derived a key for the retired profile: %v", err)
	}
	_, err = keys.DeriveAll(
		core.NewID(),
		core.NewSecret(),
		core.KDFRetired,
		passphrase,
		new([core.ShareLen]byte),
	)
	if !errors.Is(err, core.ErrKDF) {
		t.Fatalf("DeriveAll accepted the retired profile: %v", err)
	}
	header := envelope.NewHeader(core.KDFScrypt).Bytes()
	header[1] = byte(core.KDFRetired)
	if _, err = envelope.ParseHeader(header); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("an envelope header with kdf_id 2: %v", err)
	}
	payload := envelope.Payload{Meta: envelope.Meta{Text: new("x")}}
	policy := envelope.Policy{OpensMax: 1, TTLSeconds: 60}
	_, err = envelope.Seal(&keys.ContentKeys{}, envelope.NewHeader(core.KDFRetired), core.NewID(), policy, payload)
	if !errors.Is(err, core.ErrKDF) {
		t.Fatalf("sealed an envelope for the retired profile: %v", err)
	}
}

func TestOnlyProfilesZeroAndThreeAreKnown(t *testing.T) {
	t.Parallel()
	for text, valid := range map[string]bool{"0": true, "1": false, "2": false, "3": true, "4": false} {
		if _, err := core.ParseKDF(text); (err == nil) != valid {
			t.Errorf("kdf_id %s: %v", text, err)
		}
	}
	header := envelope.NewHeader(core.KDFScrypt).Bytes()
	header[1] = 1
	if _, err := envelope.ParseHeader(header); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("a header with the profile of protocol version 1: %v", err)
	}
}

func TestAK24ServerProfileMustMatchTheFragment(t *testing.T) {
	t.Parallel()
	for _, reported := range []int{int(core.KDFNone), int(core.KDFRetired), 1, 257, -255} {
		err := core.AssertProfile(core.KDFScrypt, reported)
		if !errors.Is(err, core.ErrKDFMismatch) || strings.Contains(err.Error(), "envelope") {
			t.Fatalf("server-reported kdf_id %d: %v", reported, err)
		}
	}
	if err := core.AssertProfile(core.KDFScrypt, int(core.KDFScrypt)); err != nil {
		t.Fatal(err)
	}
}
