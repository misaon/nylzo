package config

import (
	"errors"
	"testing"
)

func TestIntegerIntoRefusesANumberItsTargetCannotHold(t *testing.T) {
	t.Parallel()
	var target int32 = 7
	if err := integerInto(&target)("2147483648"); !errors.Is(err, errIntegerTooLarge) || target != 7 {
		t.Fatalf("a number past the target's range: %v, target %d", err, target)
	}
	if err := integerInto(&target)("2147483647"); err != nil || target != 2147483647 {
		t.Fatalf("the largest number the target holds: %v, target %d", err, target)
	}
}
