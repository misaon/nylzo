package vectors

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/misaon/nylzo/internal/core/keys"
)

type WordList struct {
	Language  string `json:"language"`
	SHA256    string `json:"sha256"`
	Separator string `json:"separator"`
	Words     int    `json:"words"`
}

type GeneratedPassphrase struct {
	Name       string `json:"name"`
	Language   string `json:"language"`
	Random     string `json:"random_hex"`
	Passphrase string `json:"passphrase"`
}

func (f *File) passphraseWords() error {
	for _, language := range keys.PassphraseLanguages {
		if err := f.wordList(language); err != nil {
			return err
		}
	}
	equalWords := append(bytes.Repeat([]byte{0xff}, 10), deterministic("passphrase/redraw", 10)...)
	return f.generated("five equal words are drawn again", "en", equalWords)
}

func (f *File) wordList(language string) error {
	words, layout, err := keys.PassphraseWordList(language)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(strings.Join(words, "\n") + "\n"))
	f.WordLists = append(f.WordLists, WordList{
		Language:  language,
		SHA256:    hex.EncodeToString(sum[:]),
		Separator: layout.Separator,
		Words:     layout.Words,
	})
	return f.generated(language+" words", language, deterministic("passphrase/"+language, 2*layout.Words))
}

func (f *File) generated(name, language string, random []byte) error {
	passphrase, err := keys.GeneratePassphraseFrom(bytes.NewReader(random), language)
	if err != nil {
		return err
	}
	f.Generated = append(f.Generated, GeneratedPassphrase{
		Name:       name,
		Language:   language,
		Random:     hex.EncodeToString(random),
		Passphrase: passphrase,
	})
	return nil
}
