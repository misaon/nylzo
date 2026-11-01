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
	if _, err := keys.DerivePassphraseKey(core.KDF(4), "x", &salt); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("the reserved profile 4: %v", err)
	}
	_, err := keys.DerivePassphraseKey(core.KDFScrypt, "caf\xe9", &salt)
	if !errors.Is(err, keys.ErrMalformedPassphrase) {
		t.Fatalf("scrypt accepted invalid UTF-8: %v", err)
	}
	derived, err := keys.DeriveAll(
		core.NewID(),
		core.NewSecret(),
		core.KDFScrypt,
		"caf\xe9",
		new([core.ShareLen]byte),
	)
	if !errors.Is(err, keys.ErrMalformedPassphrase) || derived != (keys.Derived{}) {
		t.Fatalf("DeriveAll must refuse before deriving anything: %v", err)
	}
}

func TestF3ContentKeysNeedTheShare(t *testing.T) {
	t.Parallel()
	id, secret := core.NewID(), core.NewSecret()
	if _, err := keys.DeriveContent(id, secret, nil, nil); !errors.Is(err, keys.ErrShare) {
		t.Fatalf("content keys without a share: %v", err)
	}
	first, err := keys.DeriveContent(id, secret, nil, new([core.ShareLen]byte))
	if err != nil {
		t.Fatal(err)
	}
	other := [core.ShareLen]byte{1}
	second, err := keys.DeriveContent(id, secret, nil, &other)
	if err != nil || first.K == second.K || first.Commitment == second.Commitment || first.K == first.Commitment {
		t.Fatalf("the share must change K and K_c, which must differ: %v", err)
	}
	verifier, err := keys.DeriveVerifier(id, secret, nil)
	if err != nil || verifier == first.K || verifier == first.Commitment {
		t.Fatalf("V must be its own key: %v", err)
	}
}
