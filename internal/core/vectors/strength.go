package vectors

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/misaon/nylzo/internal/core/keys"
)

type Strength struct {
	Name       string                 `json:"name"`
	Passphrase string                 `json:"passphrase"`
	Accepted   bool                   `json:"accepted"`
	Problem    keys.PassphraseProblem `json:"problem,omitempty"`
}

const face = "\U0001F600"

var strengthCases = []Strength{
	{"empty", "", false, keys.PassphraseShort},
	{"eleven ASCII characters", "eleven char", false, keys.PassphraseShort},
	{"twelve ASCII characters", "twelve chars", true, ""},
	{"eleven characters after NFC, twelve code points before", "café au lai", false, keys.PassphraseShort},
	{"twelve characters after NFC", "café au lait", true, ""},
	{"eleven astral characters", strings.Repeat(face, 11), false, keys.PassphraseShort},
	{"twelve equal astral characters", strings.Repeat(face, 12), false, keys.PassphraseRepetitive},
	{
		"twelve distinct astral characters",
		"\U0001F600\U0001F601\U0001F602\U0001F603\U0001F604\U0001F605\U0001F606\U0001F607\U0001F608\U0001F609\U0001F60A\U0001F60B",
		true,
		"",
	},
	{"one character twelve times", "aaaaaaaaaaaa", false, keys.PassphraseRepetitive},
	{"two characters in turn", "abababababab", false, keys.PassphraseRepetitive},
	{"six distinct characters twice", "abcdefabcdef", false, keys.PassphraseRepetitive},
	{"repetition hidden by letter case", "ABCDEFabcdef", false, keys.PassphraseRepetitive},
	{"part of a repetition", "abcdefgabcdefga", false, keys.PassphraseRepetitive},
	{"alphabet", "abcdefghijkl", false, keys.PassphraseSequential},
	{"alphabet backwards", "lkjihgfedcba", false, keys.PassphraseSequential},
	{"digits across zero", "7890123456789", false, keys.PassphraseSequential},
	{"QWERTY row", "qwertyuiopas", false, keys.PassphraseSequential},
	{"QWERTZ row in capitals", "QWERTZUIOPASDF", false, keys.PassphraseSequential},
	{"alphabet with separators", "a-b c_d.e-f g_h", false, keys.PassphraseSequential},
	{"service name twice", "nylzonylzo12", false, keys.PassphraseContext},
	{"service name with a short word", "NylzoPassword", false, keys.PassphraseContext},
	{"common password", "password1234", false, keys.PassphraseCommon},
	{"common password in capitals", "IloveYou1234", false, keys.PassphraseCommon},
	{"common keyboard walk outside the sequences", "1qaz2wsx3edc", false, keys.PassphraseCommon},
	{"generated passphrase", "k7m2-x9pz-b4tf-hq3w", true, ""},
	{"four words", "correct horse battery staple", true, ""},
}

func (f *File) strength() error {
	for _, vector := range strengthCases {
		err := keys.CheckPassphraseStrength(vector.Passphrase)
		if (err == nil) != vector.Accepted {
			return fmt.Errorf("vectors: strength case %q disagrees with the core: %w", vector.Name, err)
		}
		if weak, ok := errors.AsType[*keys.WeakPassphraseError](err); ok && weak.Problem != vector.Problem {
			return fmt.Errorf(
				"vectors: strength case %q names problem %q, the core %q",
				vector.Name,
				vector.Problem,
				weak.Problem,
			)
		}
		f.Strength = append(f.Strength, vector)
	}
	sum := sha256.Sum256([]byte(keys.CommonPassphraseList))
	f.ListHash = hex.EncodeToString(sum[:])
	return nil
}
