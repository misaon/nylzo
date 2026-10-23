package cli

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxNameBytes      = 255
	maxExtensionRunes = 16
	fallbackName      = "nylzo"
)

var (
	reservedRunes = regexp.MustCompile(`[/\\:*?"<>|]`)
	windowsDevice = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9¹²³]|lpt[0-9¹²³]|conin\$|conout\$) *(\.|$)`)
)

func invisible(r rune) bool {
	switch r {
	case 0x115F, 0x1160, 0x3164, 0xFFA0:
		return true
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

func attachmentName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if invisible(r) {
			return -1
		}
		return r
	}, norm.NFC.String(strings.ToValidUTF8(name, "")))
	clean = reservedRunes.ReplaceAllString(clean, "_")
	clean = strings.Join(strings.FieldsFunc(clean, unicode.IsSpace), " ")
	dotOrSpace := func(r rune) bool { return r == '.' || r == ' ' }
	clean = strings.TrimRightFunc(strings.TrimLeftFunc(clean, dotOrSpace), dotOrSpace)
	if windowsDevice.MatchString(clean) {
		clean = "_" + clean
	}
	if clean == "" {
		return fallbackName
	}
	if len(clean) <= maxNameBytes {
		return clean
	}
	extension := ""
	if dot := strings.LastIndex(clean, "."); dot > 0 && utf8.RuneCountInString(clean[dot+1:]) <= maxExtensionRunes {
		extension = clean[dot:]
	}
	return truncateBytes(strings.TrimSuffix(clean, extension), maxNameBytes-len(extension)) + extension
}

func truncateBytes(s string, limit int) string {
	used := 0
	for i, r := range s {
		size := utf8.RuneLen(r)
		if used+size > limit {
			return s[:i]
		}
		used += size
	}
	return s
}
