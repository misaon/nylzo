package core

import (
	"crypto/rand"
	_ "embed"
	"errors"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	MinPassphraseCharacters         = 12
	minPassphraseDistinctCharacters = 6
	passphraseContextWord           = "nylzo"
	generatedPassphraseGroups       = 4
	generatedPassphraseGroupLength  = 4
	generatedPassphraseAlphabet     = "0123456789abcdefghjkmnpqrstvwxyz"
	generatedPassphraseSeparator    = '-'
)

type PassphraseProblem string

const (
	PassphraseShort      PassphraseProblem = "short"
	PassphraseRepetitive PassphraseProblem = "repetitive"
	PassphraseSequential PassphraseProblem = "sequential"
	PassphraseContext    PassphraseProblem = "context"
	PassphraseCommon     PassphraseProblem = "common"
)

var ErrWeakPassphrase = errors.New("core: weak passphrase")

type WeakPassphraseError struct {
	Problem PassphraseProblem
}

func (e *WeakPassphraseError) Error() string {
	return "core: weak passphrase (" + string(e.Problem) + ")"
}

func (e *WeakPassphraseError) Is(target error) bool {
	return target == ErrWeakPassphrase
}

//go:embed common-passphrases.txt
var CommonPassphraseList string

var commonPassphrases = sync.OnceValue(func() map[string]struct{} {
	entries := strings.Split(strings.TrimSuffix(CommonPassphraseList, "\n"), "\n")
	set := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		set[entry] = struct{}{}
	}
	return set
})

var passphraseSequences = []string{
	"abcdefghijklmnopqrstuvwxyz",
	"01234567890123456789",
	"qwertyuiopasdfghjklzxcvbnm",
	"qwertzuiopasdfghjklyxcvbnm",
}

// CheckPassphraseStrength applies spec section 2 in its order, so both cores name the same problem.
func CheckPassphraseStrength(passphrase string) error {
	if !utf8.ValidString(passphrase) {
		return ErrMalformedPassphrase
	}
	normalized := norm.NFC.String(passphrase)
	if utf8.RuneCountInString(normalized) < MinPassphraseCharacters {
		return &WeakPassphraseError{PassphraseShort}
	}
	folded := foldASCII(normalized)
	switch {
	case distinctCharacters(folded) < minPassphraseDistinctCharacters || repeatsShorterPart([]rune(folded)):
		return &WeakPassphraseError{PassphraseRepetitive}
	case followsSequence(withoutSeparators(folded)):
		return &WeakPassphraseError{PassphraseSequential}
	case utf8.RuneCountInString(strings.ReplaceAll(folded, passphraseContextWord, "")) < MinPassphraseCharacters:
		return &WeakPassphraseError{PassphraseContext}
	}
	if _, common := commonPassphrases()[folded]; common {
		return &WeakPassphraseError{PassphraseCommon}
	}
	return nil
}

func GeneratePassphrase() string {
	for {
		var random [generatedPassphraseGroups * generatedPassphraseGroupLength]byte
		rand.Read(random[:])
		var builder strings.Builder
		for index, value := range random {
			if index > 0 && index%generatedPassphraseGroupLength == 0 {
				builder.WriteByte(generatedPassphraseSeparator)
			}
			builder.WriteByte(generatedPassphraseAlphabet[value&31])
		}
		clear(random[:])
		if candidate := builder.String(); CheckPassphraseStrength(candidate) == nil {
			return candidate
		}
	}
}

func foldASCII(text string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		return character
	}, text)
}

func distinctCharacters(text string) int {
	seen := map[rune]struct{}{}
	for _, character := range text {
		seen[character] = struct{}{}
	}
	return len(seen)
}

func repeatsShorterPart(characters []rune) bool {
	for period := 1; period <= len(characters)/2; period++ {
		repeats := true
		for index := period; index < len(characters); index++ {
			if characters[index] != characters[index-period] {
				repeats = false
				break
			}
		}
		if repeats {
			return true
		}
	}
	return false
}

func withoutSeparators(text string) string {
	return strings.Map(func(character rune) rune {
		if strings.ContainsRune(" -_.", character) {
			return -1
		}
		return character
	}, text)
}

func followsSequence(text string) bool {
	backwards := []rune(text)
	slices.Reverse(backwards)
	return slices.ContainsFunc(passphraseSequences, func(sequence string) bool {
		return strings.Contains(sequence, text) || strings.Contains(sequence, string(backwards))
	})
}
