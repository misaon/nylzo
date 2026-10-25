package vectors

import (
	"fmt"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

type Strength struct {
	Name       string `json:"name"`
	Passphrase string `json:"passphrase"`
	Accepted   bool   `json:"accepted"`
}

const face = "\U0001F600"

var strengthCases = []Strength{
	{"empty", "", false},
	{"eleven ASCII characters", "eleven char", false},
	{"twelve ASCII characters", "twelve chars", true},
	{"eleven characters after NFC, twelve code points before", "cafe\u0301 au lai", false},
	{"twelve characters after NFC", "cafe\u0301 au lait", true},
	{"eleven astral characters", strings.Repeat(face, 11), false},
	{"twelve astral characters", strings.Repeat(face, 12), true},
}

func (f *File) strength() error {
	for _, vector := range strengthCases {
		if err := core.CheckPassphraseStrength(vector.Passphrase); (err == nil) != vector.Accepted {
			return fmt.Errorf("vectors: strength case %q disagrees with the core: %w", vector.Name, err)
		}
		f.Strength = append(f.Strength, vector)
	}
	return nil
}
