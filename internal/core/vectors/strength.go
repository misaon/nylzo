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
	{"twelve ASCII characters", "twelve chars", false, keys.PassphraseShort},
	{"fourteen ASCII characters", "fourteen chars", false, keys.PassphraseShort},
	{"fifteen ASCII characters", "fifteen chars x", true, ""},
	{"fourteen characters after NFC, fifteen code points before", "cafe\u0301 au laitxy", false, keys.PassphraseShort},
	{"fifteen characters after NFC", "cafe\u0301 au laitxyz", true, ""},
	{"fourteen astral characters", strings.Repeat(face, 14), false, keys.PassphraseShort},
	{"fifteen equal astral characters", strings.Repeat(face, 15), false, keys.PassphraseRepetitive},
	{
		"fifteen distinct astral characters",
		"\U0001F600\U0001F601\U0001F602\U0001F603\U0001F604\U0001F605\U0001F606\U0001F607\U0001F608\U0001F609\U0001F60A\U0001F60B\U0001F60C\U0001F60D\U0001F60E",
		true,
		"",
	},
	{"one character fifteen times", "aaaaaaaaaaaaaaa", false, keys.PassphraseRepetitive},
	{"two characters in turn", "abababababababab", false, keys.PassphraseRepetitive},
	{"six distinct characters repeated", "abcdefabcdefabc", false, keys.PassphraseRepetitive},
	{"repetition hidden by letter case", "ABCDEFabcdefABC", false, keys.PassphraseRepetitive},
	{"part of a repetition", "abcdefgabcdefgab", false, keys.PassphraseRepetitive},
	{"alphabet", "abcdefghijklmno", false, keys.PassphraseSequential},
	{"alphabet backwards", "onmlkjihgfedcba", false, keys.PassphraseSequential},
	{"digits across zero", "567890123456789", false, keys.PassphraseSequential},
	{"QWERTY row", "qwertyuiopasdfg", false, keys.PassphraseSequential},
	{"QWERTZ row in capitals", "QWERTZUIOPASDFGH", false, keys.PassphraseSequential},
	{"alphabet with separators", "a-b c_d.e-f g_h.i j", false, keys.PassphraseSequential},
	{"service name twice", "nylzonylzo12345", false, keys.PassphraseContext},
	{"service name with a short word", "NylzoPassword12", false, keys.PassphraseContext},
	{"common password", "polniypizdec0211", false, keys.PassphraseCommon},
	{"common password in capitals", "PolniyPizdec0211", false, keys.PassphraseCommon},
	{"common keyboard walk outside the sequences", "1q2w3e4r5t6y7u8i9o0p", false, keys.PassphraseCommon},
	{"code point never assigned", "a passphrase \u0378 with a gap", false, keys.PassphraseUnassigned},
	{
		"Kirat Rai pair that Unicode 16 composes",
		"kirat rai \U00016D63\U00016D67 composes",
		false,
		keys.PassphraseUnassigned,
	},
	{"emoji of Unicode 16", "tired face \U0001FAE9 is new", false, keys.PassphraseUnassigned},
	{"ideograph of Unicode 15.1", "ideograph \U0002EBF0 from 15.1", true, ""},
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
	assigned := sha256.Sum256([]byte(keys.AssignedCodePointList))
	f.AssignedHash = hex.EncodeToString(assigned[:])
	f.AssignedVersion = keys.AssignedUnicodeVersion
	return nil
}
