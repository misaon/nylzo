package core_test

import (
	"regexp"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestGeneratedPassphrasesAreStrongAndWellFormed(t *testing.T) {
	t.Parallel()
	format := regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{4}(-[0-9a-hjkmnp-tv-z]{4}){3}$`)
	seen := map[string]bool{}
	for range 500 {
		passphrase := core.GeneratePassphrase()
		if !format.MatchString(passphrase) {
			t.Fatalf("generated passphrase %q has the wrong format", passphrase)
		}
		if err := core.CheckPassphraseStrength(passphrase); err != nil {
			t.Fatalf("generated passphrase %q is refused: %v", passphrase, err)
		}
		if seen[passphrase] {
			t.Fatalf("generator repeated %q", passphrase)
		}
		seen[passphrase] = true
	}
}

func TestMayHoldFileFollowsThePadding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		objectBytes int64
		mayHold     bool
	}{
		{core.PadText, false},
		{15 * core.PadText, false},
		{core.PadFile, true},
		{3 * core.PadFile, true},
		{3*core.PadFile + core.PadText, false},
		{core.PadFile + core.HeaderLen + core.TagLen, false},
		{0, false},
	}
	for _, testCase := range cases {
		if got := core.MayHoldFile(testCase.objectBytes); got != testCase.mayHold {
			t.Fatalf("MayHoldFile(%d) = %v", testCase.objectBytes, got)
		}
	}
}
