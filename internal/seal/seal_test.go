package seal_test

import (
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

func TestAWrapOpensOnlyForItsKeyLocatorAndPurpose(t *testing.T) {
	t.Parallel()
	key, wrapping := seal.NewKey(), seal.NewKey()
	locator, elsewhere := seal.LocatorOf([]byte("message")), seal.LocatorOf([]byte("another message"))
	wrapped := seal.Wrap(wrapping, key, locator, seal.Read)
	if opened, err := seal.Unwrap(wrapping, wrapped, locator, seal.Read); err != nil || opened != key {
		t.Fatalf("unwrap: %v", err)
	}
	for name, err := range map[string]error{
		"another key":     func() error { _, err := seal.Unwrap(seal.NewKey(), wrapped, locator, seal.Read); return err }(),
		"another locator": func() error { _, err := seal.Unwrap(wrapping, wrapped, elsewhere, seal.Read); return err }(),
		"another purpose": func() error { _, err := seal.Unwrap(wrapping, wrapped, locator, seal.Manage); return err }(),
	} {
		if err == nil {
			t.Errorf("a wrap opened with %s", name)
		}
	}
	if len(wrapped) != seal.WrapLen || len(seal.Record(key, locator, make([]byte, 103))) != seal.RecordBlock {
		t.Fatal("wraps and records must have fixed lengths")
	}
}
