package keys_test

import (
	"regexp"
	"testing"

	"github.com/misaon/nylzo/internal/core/keys"
)

func TestGeneratedPassphrasesAreStrongAndWellFormed(t *testing.T) {
	t.Parallel()
	format := regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{4}(-[0-9a-hjkmnp-tv-z]{4}){3}$`)
	seen := map[string]bool{}
	for range 500 {
		passphrase := keys.GeneratePassphrase()
		if !format.MatchString(passphrase) {
			t.Fatalf("generated passphrase %q has the wrong format", passphrase)
		}
		if err := keys.CheckPassphraseStrength(passphrase); err != nil {
			t.Fatalf("generated passphrase %q is refused: %v", passphrase, err)
		}
		if seen[passphrase] {
			t.Fatalf("generator repeated %q", passphrase)
		}
		seen[passphrase] = true
	}
}
