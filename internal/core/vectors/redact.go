package vectors

import (
	"fmt"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

type Redact struct {
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Redacted string `json:"redacted"`
}

func (f *File) redact() error {
	secret := deterministic32("s/redact")
	encodedSecret := core.Encode(secret[:])
	link := core.FormatLink(base, deterministicID("redact"), core.KDFNone, secret)
	percentLink := strings.NewReplacer(":", "%3A", "/", "%2F", "#", "%23").Replace(link)
	cases := []struct{ name, reason string }{
		{"plain", "phishing for bank logins"},
		{"empty", ""},
		{"link", "see " + link + " please"},
		{"hash-run", "see https://x.example/p#token-abc def"},
		{"bare-secret", "bare " + encodedSecret + " end"},
		{"glued-secret", "key_1.0." + encodedSecret},
		{"secret-21-chars", "short " + encodedSecret[:21] + " stays"},
		{"secret-22-chars", "long " + encodedSecret[:22] + " goes"},
		{"percent-link", "https://safelinks.example/?url=" + percentLink},
		{"percent-hash-once", "see p%23token-abc def"},
		{"percent-hash-twice", "see p%2523token-abc def"},
		{"percent-hash-three-times", "see p%252523token-abc def"},
		{"percent-hash-four-times", "see p%25252523token-abc def"},
		{"secret-encoded-three-times", "key " + percentEncode(encodedSecret, 3)},
		{"controls-and-bidi", "a\u009bb\u202ec\u007fd\u2066e%E2%80%AEf\nnext"},
		{"invalid-utf8-escape", "bad %FF%FE bytes %C3"},
		{"replacement-character", "a\uFFFDb"},
		{"zero-width-split", encodedSecret[:11] + "\u200b" + encodedSecret[11:]},
		{"emoji", "\U0001F510 secret"},
	}
	for _, vector := range cases {
		redacted := core.RedactReason(vector.reason)
		if len(redacted) >= core.MaxReasonBytes {
			return fmt.Errorf("redact %s: the case must stay under the length limit", vector.name)
		}
		f.Redact = append(f.Redact, Redact{Name: vector.name, Reason: vector.reason, Redacted: redacted})
	}
	return nil
}

func percentEncode(text string, times int) string {
	for range times {
		var builder strings.Builder
		for _, octet := range []byte(text) {
			fmt.Fprintf(&builder, "%%%02X", octet)
		}
		text = builder.String()
	}
	return text
}
