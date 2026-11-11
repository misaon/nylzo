package seal_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"math/bits"
	"testing"

	"github.com/misaon/nylzo/internal/seal"
)

func TestPaddedSizeFollowsPadme(t *testing.T) {
	t.Parallel()
	for size, want := range map[int64]int64{9: 10, 4096: 4096, 4097: 4352, 100000: 100352, 5341184: 5373952} {
		if got := seal.PaddedSize(size); got != want {
			t.Errorf("PaddedSize(%d) = %d, want %d", size, got, want)
		}
	}
	for size := int64(4096); size <= 6<<20; size += 4093 {
		padded := seal.PaddedSize(size)
		exponent := bits.Len64(uint64(size)) - 1
		significant := bits.Len64(uint64(exponent))
		if padded < size || float64(padded-size) > 0.12*float64(size) ||
			bits.Len64(uint64(padded))-bits.TrailingZeros64(uint64(padded)) > significant+1 {
			t.Fatalf("PaddedSize(%d) = %d leaks more than the top bits or costs more than 12 %%", size, padded)
		}
	}
}

// F10: the record key seals the fields and wraps the blob key only through keys of their own.
func TestRecordKeyServesEachPurposeThroughItsOwnKey(t *testing.T) {
	t.Parallel()
	recordKey, blobKey := seal.NewKey(), seal.NewKey()
	locator := seal.LocatorOf([]byte("message"))
	aead := func(key seal.Key) cipher.AEAD {
		block, err := aes.NewCipher(key[:])
		if err != nil {
			t.Fatal(err)
		}
		gcm, err := cipher.NewGCMWithRandomNonce(block)
		if err != nil {
			t.Fatal(err)
		}
		return gcm
	}
	aad := func(label string) []byte { return append(locator[:len(locator):len(locator)], label...) }
	if _, err := aead(
		recordKey,
	).Open(nil, nil, seal.Record(recordKey, locator, []byte("fields")), aad("record")); err == nil {
		t.Fatal("the fields are sealed under the record key itself")
	}
	if _, err := aead(
		recordKey,
	).Open(nil, nil, seal.WrapBlobKey(recordKey, blobKey, locator), aad("blob")); err == nil {
		t.Fatal("the blob key is wrapped under the record key itself")
	}
}

func TestAWrapOpensOnlyWithBothKeysForItsLocatorAndPurpose(t *testing.T) {
	t.Parallel()
	recordKey, pepperKey := seal.NewKey(), seal.NewKey()
	publicKey := []byte("a public key that a request carries")
	locator, elsewhere := seal.LocatorOf([]byte("message")), seal.LocatorOf([]byte("another message"))
	wrapped := seal.WrapRecordKey(pepperKey, publicKey, recordKey, locator, seal.Read)
	unwrap := func(pepper seal.Key, public []byte, at seal.Locator, purpose seal.Purpose) error {
		opened, err := seal.UnwrapRecordKey(pepper, public, wrapped, at, purpose)
		if err == nil && opened != recordKey {
			t.Fatal("the wrap opened to another key")
		}
		return err
	}
	if err := unwrap(pepperKey, publicKey, locator, seal.Read); err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	for name, err := range map[string]error{
		"the public key alone":      unwrap(seal.Key{}, publicKey, locator, seal.Read),
		"another pepper key":        unwrap(seal.NewKey(), publicKey, locator, seal.Read),
		"the pepper key alone":      unwrap(pepperKey, nil, locator, seal.Read),
		"another public key":        unwrap(pepperKey, []byte("another public key"), locator, seal.Read),
		"another locator":           unwrap(pepperKey, publicKey, elsewhere, seal.Read),
		"another purpose":           unwrap(pepperKey, publicKey, locator, seal.Manage),
		"the inner wrap on its own": openInner(t, wrapped, publicKey, locator),
	} {
		if err == nil {
			t.Errorf("a wrap opened with %s", name)
		}
	}
	if len(wrapped) != seal.SealedWrapLen ||
		len(seal.Record(recordKey, locator, make([]byte, 103))) != seal.RecordBlock {
		t.Fatal("wraps and records must have fixed lengths")
	}
}

// openInner treats a stored wrap as the bare wrap of an earlier release, which the public key alone opened.
func openInner(t *testing.T, stored, publicKey []byte, locator seal.Locator) error {
	t.Helper()
	derived, err := hkdf.Key(sha256.New, publicKey, locator[:], "nylzo/v1/record/read", seal.KeyLen)
	if err != nil {
		t.Fatal(err)
	}
	_, err = newGCM(t, seal.Key(derived)).Open(nil, nil, stored, aad(locator, "read"))
	return err
}

// legacyWrap builds a wrap as nylzo 0.13 and earlier stored it, from spec §5.2 of that release.
func legacyWrap(t *testing.T, publicKey []byte, recordKey seal.Key, locator seal.Locator) []byte {
	t.Helper()
	derived, err := hkdf.Key(sha256.New, publicKey, locator[:], "nylzo/v1/record/read", seal.KeyLen)
	if err != nil {
		t.Fatal(err)
	}
	return newGCM(t, seal.Key(derived)).Seal(nil, nil, recordKey[:], aad(locator, "read"))
}

func TestResealWrapMovesEveryWrapUnderTheCurrentPepperKey(t *testing.T) {
	t.Parallel()
	recordKey, previous, current := seal.NewKey(), seal.NewKey(), seal.NewKey()
	publicKey := []byte("pk_R")
	locator := seal.LocatorOf([]byte("message"))
	opensWith := func(wrapped []byte, pepperKey seal.Key) bool {
		opened, err := seal.UnwrapRecordKey(pepperKey, publicKey, wrapped, locator, seal.Read)
		return err == nil && opened == recordKey
	}
	legacy := legacyWrap(t, publicKey, recordKey, locator)
	if len(legacy) != seal.WrapLen {
		t.Fatalf("a wrap of an earlier release has %d bytes", len(legacy))
	}
	changed, err := seal.ResealWrap(current, nil, &legacy, locator, seal.Read)
	if err != nil || !changed || !opensWith(legacy, current) {
		t.Fatalf("a wrap of an earlier release was not sealed: changed %t, %v", changed, err)
	}
	underPrevious := seal.WrapRecordKey(previous, publicKey, recordKey, locator, seal.Read)
	resealed := bytes.Clone(underPrevious)
	changed, err = seal.ResealWrap(current, []seal.Key{previous}, &resealed, locator, seal.Read)
	if err != nil || !changed || !opensWith(resealed, current) || opensWith(resealed, previous) {
		t.Fatalf("a wrap of the previous pepper was not sealed again: changed %t, %v", changed, err)
	}
	kept := bytes.Clone(resealed)
	changed, err = seal.ResealWrap(current, []seal.Key{previous}, &kept, locator, seal.Read)
	if err != nil || changed || !bytes.Equal(kept, resealed) {
		t.Fatalf("a wrap under the current pepper changed: %t, %v", changed, err)
	}
	kept = bytes.Clone(underPrevious)
	changed, err = seal.ResealWrap(current, nil, &kept, locator, seal.Read)
	if !errors.Is(err, seal.ErrOpen) || changed || !bytes.Equal(kept, underPrevious) {
		t.Fatalf("a wrap that no pepper key opens must stay as it is: %t, %v", changed, err)
	}
}

func newGCM(t *testing.T, key seal.Key) cipher.AEAD {
	t.Helper()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		t.Fatal(err)
	}
	return gcm
}

func aad(locator seal.Locator, label string) []byte {
	return append(locator[:len(locator):len(locator)], label...)
}
