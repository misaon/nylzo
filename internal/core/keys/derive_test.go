package keys_test

import (
	"errors"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/keys"
)

func TestPassphraseIsCheckedBeforeDerivation(t *testing.T) {
	t.Parallel()
	var salt [32]byte
	if key, err := keys.DerivePassphraseKey(core.KDFNone, "caf\xe9", &salt); err != nil || key != nil {
		t.Fatalf("profile 0 must ignore any passphrase: %x %v", key, err)
	}
	if _, err := keys.DerivePassphraseKey(core.KDF(3), "x", &salt); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("an unknown profile: %v", err)
	}
	_, err := keys.DerivePassphraseKey(core.KDFScrypt, "caf\xe9", &salt)
	if !errors.Is(err, keys.ErrMalformedPassphrase) {
		t.Fatalf("scrypt accepted invalid UTF-8: %v", err)
	}
	link, _, err := keys.DeriveAll(core.NewID(), core.NewSecret(), core.KDFScrypt, "caf\xe9")
	if !errors.Is(err, keys.ErrMalformedPassphrase) || link != (keys.LinkKeys{}) {
		t.Fatalf("DeriveAll must refuse before deriving anything: %v", err)
	}
}
