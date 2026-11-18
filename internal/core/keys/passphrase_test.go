package keys_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core/keys"
)

func TestGeneratedPassphrasesAreStrongWordsOfTheirLanguage(t *testing.T) {
	t.Parallel()
	for _, language := range keys.PassphraseLanguages {
		words, layout, err := keys.PassphraseWordList(language)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for range 100 {
			passphrase, generateErr := keys.GeneratePassphrase(language)
			if generateErr != nil {
				t.Fatal(generateErr)
			}
			chosen := strings.Split(passphrase, layout.Separator)
			if len(chosen) != layout.Words || slices.ContainsFunc(chosen, func(word string) bool {
				return !slices.Contains(words, word)
			}) {
				t.Fatalf("%s: generated passphrase %q is not %d words of the list", language, passphrase, layout.Words)
			}
			if strengthErr := keys.CheckPassphraseStrength(passphrase); strengthErr != nil {
				t.Fatalf("%s: generated passphrase %q is refused: %v", language, passphrase, strengthErr)
			}
			if seen[passphrase] {
				t.Fatalf("%s: generator repeated %q", language, passphrase)
			}
			seen[passphrase] = true
		}
	}
}
