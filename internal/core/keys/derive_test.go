package keys_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/keys"
)

func TestPassphraseIsCheckedBeforeDerivation(t *testing.T) {
	t.Parallel()
	secret := core.NewSecret()
	salt, err := keys.PassphraseSalt(secret)
	if err != nil {
		t.Fatal(err)
	}
	password, err := keys.Password(secret, core.KDFNone, "caf\xe9")
	if err != nil || !bytes.Equal(password, salt[:]) {
		t.Fatalf("profile 0 must ignore any passphrase: %x %v", password, err)
	}
	if _, err = keys.Password(secret, core.KDF(4), "x"); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("the reserved profile 4: %v", err)
	}
	if _, err = keys.Password(secret, core.KDFScrypt, "caf\xe9"); !errors.Is(err, keys.ErrMalformedPassphrase) {
		t.Fatalf("invalid UTF-8 became an OPRF input: %v", err)
	}
	decomposed := strings.Repeat("e\u0301", keys.MaxPassphraseBytes/2) + "x"
	if password, err = keys.Password(secret, core.KDFScrypt, decomposed); err != nil ||
		len(password) != keys.PassphraseSaltLen+keys.MaxPassphraseBytes {
		t.Fatalf("the longest passphrase, counted after NFC: %d %v", len(password), err)
	}
	if _, err = keys.Password(secret, core.KDFScrypt, decomposed+"x"); !errors.Is(err, keys.ErrLongPassphrase) {
		t.Fatalf("a passphrase one byte too long: %v", err)
	}
	if _, err = keys.OPAQUEConfig(core.NewID(), core.KDF(4)); !errors.Is(err, core.ErrKDF) {
		t.Fatalf("a configuration for the reserved profile 4: %v", err)
	}
}

func TestF3ContentKeysNeedTheShare(t *testing.T) {
	t.Parallel()
	id, secret, exportKey := core.NewID(), core.NewSecret(), new(keys.ExportKey)
	if _, err := keys.DeriveContent(id, secret, exportKey, nil); !errors.Is(err, keys.ErrShare) {
		t.Fatalf("content keys without a share: %v", err)
	}
	first, err := keys.DeriveContent(id, secret, exportKey, new([core.ShareLen]byte))
	if err != nil {
		t.Fatal(err)
	}
	other := [core.ShareLen]byte{1}
	second, err := keys.DeriveContent(id, secret, exportKey, &other)
	if err != nil || first.K == second.K || first.Commitment == second.Commitment || first.K == first.Commitment {
		t.Fatalf("the share must change K and K_c, which must differ: %v", err)
	}
	third, err := keys.DeriveContent(id, secret, &keys.ExportKey{1}, new([core.ShareLen]byte))
	if err != nil || third.K == first.K {
		t.Fatalf("the export key must change K: %v", err)
	}
}
