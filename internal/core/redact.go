package core

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	MaxReasonBytes     = 1000
	maxReasonJSONBytes = 1200
	reasonDecodePasses = 3
	maxJSONEscape      = len(`\u0000`)
	maxReportBodyBytes = 4096
)

var (
	percentEscape = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)
	linkFragment  = regexp.MustCompile(`#\S*`)
	tokenRun      = regexp.MustCompile(fmt.Sprintf(`[A-Za-z0-9_-]{%d,}`, base64.RawURLEncoding.EncodedLen(IDLen)))
)

func redactUnbounded(reason string) string {
	reason = strings.Map(visibleRune, percentDecode(reason))
	reason = linkFragment.ReplaceAllString(reason, "[redacted]")
	return tokenRun.ReplaceAllString(reason, "[redacted]")
}

func ReasonTooLong(reason string) bool {
	redacted := redactUnbounded(reason)
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(
		map[string]string{"id": strings.Repeat("A", base64.RawURLEncoding.EncodedLen(IDLen)), "reason": redacted},
	); err != nil {
		return true
	}
	return len(redacted) > MaxReasonBytes || body.Len()-1 > maxReportBodyBytes
}

func RedactReason(reason string) string {
	reason = redactUnbounded(reason)
	if len(reason) > MaxReasonBytes {
		reason = strings.ToValidUTF8(reason[:MaxReasonBytes], "")
	}
	for {
		encoded, _ := json.Marshal(reason)
		if len(encoded) <= maxReasonJSONBytes {
			return reason
		}
		runes := []rune(reason)
		reason = string(runes[:len(runes)-max(1, (len(encoded)-maxReasonJSONBytes)/maxJSONEscape)])
	}
}

func percentDecode(text string) string {
	for range reasonDecodePasses {
		decoded := percentEscape.ReplaceAllStringFunc(text, func(esc string) string {
			b, _ := hex.DecodeString(esc[1:])
			return string(b)
		})
		if decoded == text {
			break
		}
		text = decoded
	}
	return strings.ReplaceAll(
		strings.ToValidUTF8(text, string(unicode.ReplacementChar)),
		string(unicode.ReplacementChar),
		"",
	)
}

func visibleRune(r rune) rune {
	switch {
	case unicode.Is(unicode.Cf, r):
		return -1
	case unicode.IsControl(r):
		return ' '
	}
	return r
}
