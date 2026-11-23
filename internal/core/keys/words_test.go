package keys_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/misaon/nylzo/internal/core/keys"
)

func TestAnUnknownLanguageHasNoPassphrase(t *testing.T) {
	t.Parallel()
	if _, _, err := keys.PassphraseWordList("tlh"); !errors.Is(err, keys.ErrUnknownPassphraseLanguage) {
		t.Fatalf("word list: got %v", err)
	}
	if _, err := keys.GeneratePassphrase("tlh"); !errors.Is(err, keys.ErrUnknownPassphraseLanguage) {
		t.Fatalf("passphrase: got %v", err)
	}
}

func TestGeneratingStopsWhenTheRandomSourceRunsOut(t *testing.T) {
	t.Parallel()
	if _, err := keys.GeneratePassphraseFrom(bytes.NewReader(nil), "en"); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want io.EOF", err)
	}
}

func TestAPassphraseThatIsNotUTF8IsMalformed(t *testing.T) {
	t.Parallel()
	if err := keys.CheckPassphraseStrength(
		"\xff long enough passphrase",
	); !errors.Is(
		err,
		keys.ErrMalformedPassphrase,
	) {
		t.Fatalf("got %v, want ErrMalformedPassphrase", err)
	}
}
