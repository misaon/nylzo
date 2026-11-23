package keys

import (
	"crypto/rand"
	"embed"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"unicode"
)

const (
	passphraseWordListLength = 2048
	passphraseWords          = 5
	chinesePassphraseWords   = 6
)

var PassphraseLanguages = []string{"en", "cs", "de", "es", "fr", "id", "it", "ja", "pl", "pt-BR", "sk", "zh-Hans"}

var ErrUnknownPassphraseLanguage = errors.New("core: unknown passphrase language")

type PassphraseLayout struct {
	Separator string
	Words     int
}

//go:embed words/*.txt
var wordFiles embed.FS

var wordLists = sync.OnceValue(func() map[string][]string { return loadWordLists(wordFiles) })

func loadWordLists(files fs.FS) map[string][]string {
	lists := make(map[string][]string, len(PassphraseLanguages))
	for _, language := range PassphraseLanguages {
		text, err := fs.ReadFile(files, "words/"+strings.ToLower(language)+".txt")
		if err != nil {
			panic("keys: no word list for " + language)
		}
		words := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
		distinct := len(slices.Compact(slices.Sorted(slices.Values(words)))) == len(words)
		if len(words) != passphraseWordListLength || !distinct || slices.ContainsFunc(words, malformedWord) {
			panic("keys: the word list for " + language + " does not hold 2048 distinct words without spaces")
		}
		lists[language] = words
	}
	return lists
}

func malformedWord(word string) bool {
	return word == "" || strings.ContainsFunc(word, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	})
}

func passphraseLayout(language string) PassphraseLayout {
	switch language {
	case "ja":
		return PassphraseLayout{Separator: "\u3000", Words: passphraseWords}
	case "zh-Hans":
		return PassphraseLayout{Separator: " ", Words: chinesePassphraseWords}
	default:
		return PassphraseLayout{Separator: "-", Words: passphraseWords}
	}
}

func PassphraseWordList(language string) ([]string, PassphraseLayout, error) {
	words, ok := wordLists()[language]
	if !ok {
		return nil, PassphraseLayout{}, ErrUnknownPassphraseLanguage
	}
	return slices.Clone(words), passphraseLayout(language), nil
}

func GeneratePassphrase(language string) (string, error) {
	return GeneratePassphraseFrom(rand.Reader, language)
}

func GeneratePassphraseFrom(random io.Reader, language string) (string, error) {
	words, ok := wordLists()[language]
	if !ok {
		return "", ErrUnknownPassphraseLanguage
	}
	layout := passphraseLayout(language)
	indexes := make([]byte, 2*layout.Words)
	defer clear(indexes)
	chosen := make([]string, layout.Words)
	for {
		if _, err := io.ReadFull(random, indexes); err != nil {
			return "", err
		}
		for i := range chosen {
			chosen[i] = words[binary.BigEndian.Uint16(indexes[2*i:])%passphraseWordListLength]
		}
		if candidate := strings.Join(chosen, layout.Separator); CheckPassphraseStrength(candidate) == nil {
			return candidate, nil
		}
	}
}
