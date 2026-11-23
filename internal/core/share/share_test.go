package share_test

import (
	"crypto/ecdh"
	"crypto/hpke"
	"crypto/mlkem"
	"errors"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/share"
)

func TestASeedOfAnotherLengthIsNoShareKey(t *testing.T) {
	t.Parallel()
	if _, err := share.NewKeyFromSeed(make([]byte, 31)); !errors.Is(err, share.ErrShareKey) {
		t.Fatalf("got %v, want ErrShareKey", err)
	}
}

func TestWrapAndUnwrapRefuseAKeyOfAnotherKEM(t *testing.T) {
	t.Parallel()
	other, err := hpke.DHKEM(ecdh.X25519()).GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = share.Wrap(new([core.ShareLen]byte), other.PublicKey()); !errors.Is(err, share.ErrShareKey) {
		t.Fatalf("wrap: got %v, want ErrShareKey", err)
	}
	if _, err = share.Unwrap(other, new([share.WrappedLen]byte)); !errors.Is(err, share.ErrShareKey) {
		t.Fatalf("unwrap: got %v, want ErrShareKey", err)
	}
}

func TestWrapFailsForAnX25519HalfOfSmallOrder(t *testing.T) {
	t.Parallel()
	encoded := share.NewKey().PublicKey().Bytes()
	clear(encoded[mlkem.EncapsulationKeySize768:])
	key, err := hpke.MLKEM768X25519().NewPublicKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = share.ParseKey(share.FormatKey(key)); !errors.Is(err, share.ErrShareKey) {
		t.Fatalf("parse: got %v, want ErrShareKey", err)
	}
	if _, err = share.Wrap(new([core.ShareLen]byte), key); err == nil {
		t.Fatal("wrapped a share to an X25519 half of small order")
	}
}

func TestAWrappedShareOpensOnlyWithItsKey(t *testing.T) {
	t.Parallel()
	expiry := *core.NewSecret()
	key := share.NewKey()
	wrapped, err := share.Wrap(&expiry, key.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if unwrapped, unwrapErr := share.Unwrap(key, &wrapped); unwrapErr != nil || unwrapped != expiry {
		t.Fatalf("the own key: %v", unwrapErr)
	}
	if _, err = share.Unwrap(share.NewKey(), &wrapped); !errors.Is(err, share.ErrWrapped) {
		t.Fatalf("another key: got %v, want ErrWrapped", err)
	}
}
