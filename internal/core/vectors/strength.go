package vectors

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

type Strength struct {
	Name       string `json:"name"`
	Passphrase string `json:"passphrase"`
	Accepted   bool   `json:"accepted"`
	Problem    string `json:"problem,omitempty"`
}

const face = "\U0001F600"

var strengthCases = []Strength{
	{"empty", "", false, "short"},
	{"eleven ASCII characters", "eleven char", false, "short"},
	{"twelve ASCII characters", "twelve chars", true, ""},
	{"eleven characters after NFC, twelve code points before", "café au lai", false, "short"},
	{"twelve characters after NFC", "café au lait", true, ""},
	{"eleven astral characters", strings.Repeat(face, 11), false, "short"},
	{"twelve equal astral characters", strings.Repeat(face, 12), false, "repetitive"},
	{"twelve distinct astral characters", "\U0001F600\U0001F601\U0001F602\U0001F603\U0001F604\U0001F605\U0001F606\U0001F607\U0001F608\U0001F609\U0001F60A\U0001F60B", true, ""},
	{"one character twelve times", "aaaaaaaaaaaa", false, "repetitive"},
	{"two characters in turn", "abababababab", false, "repetitive"},
	{"six distinct characters twice", "abcdefabcdef", false, "repetitive"},
	{"repetition hidden by letter case", "ABCDEFabcdef", false, "repetitive"},
	{"part of a repetition", "abcdefgabcdefga", false, "repetitive"},
	{"alphabet", "abcdefghijkl", false, "sequential"},
	{"alphabet backwards", "lkjihgfedcba", false, "sequential"},
	{"digits across zero", "7890123456789", false, "sequential"},
	{"QWERTY row", "qwertyuiopas", false, "sequential"},
	{"QWERTZ row in capitals", "QWERTZUIOPASDF", false, "sequential"},
	{"alphabet with separators", "a-b c_d.e-f g_h", false, "sequential"},
	{"service name twice", "nylzonylzo12", false, "context"},
	{"service name with a short word", "NylzoPassword", false, "context"},
	{"common password", "password1234", false, "common"},
	{"common password in capitals", "IloveYou1234", false, "common"},
	{"common keyboard walk outside the sequences", "1qaz2wsx3edc", false, "common"},
	{"generated passphrase", "k7m2-x9pz-b4tf-hq3w", true, ""},
	{"four words", "correct horse battery staple", true, ""},
}

func (f *File) strength() error {
	for _, vector := range strengthCases {
		err := core.CheckPassphraseStrength(vector.Passphrase)
		if (err == nil) != vector.Accepted {
			return fmt.Errorf("vectors: strength case %q disagrees with the core: %w", vector.Name, err)
		}
		if weak, ok := errors.AsType[*core.WeakPassphraseError](err); ok && string(weak.Problem) != vector.Problem {
			return fmt.Errorf("vectors: strength case %q names problem %q, the core %q", vector.Name, vector.Problem, weak.Problem)
		}
		f.Strength = append(f.Strength, vector)
	}
	sum := sha256.Sum256([]byte(core.CommonPassphraseList))
	f.ListHash = hex.EncodeToString(sum[:])
	return nil
}
